// Package observe 记录 workflow 的执行事实，但不认识小说角色或当前阶段结构。
// 业务层只描述 execution、operation 和 event；具体落盘方式由 Sink 决定，避免 workflow 绑定某个观测平台。
package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const schemaVersion = 1

// Kind 只表达长期稳定的技术语义。Writer、Editor、Commit 等易变概念继续放在 name/attributes 中。
type Kind string

const (
	KindWorkflow   Kind = "workflow"
	KindModel      Kind = "model"
	KindStructured Kind = "structured"
)

// Attrs 保存诊断维度，不把 workflow 当前的数据模型固化进 Observation schema。
type Attrs map[string]any

// Record 是追加式 Observation 的稳定线协议；当前写 JSONL，后续可由其他 Sink 映射到 OTel 等后端。
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	At            time.Time `json:"at"`
	Type          string    `json:"type"`
	ExecutionID   string    `json:"execution_id,omitempty"`
	OperationID   string    `json:"operation_id,omitempty"`
	ParentID      string    `json:"parent_id,omitempty"`
	Name          string    `json:"name,omitempty"`
	Kind          Kind      `json:"kind,omitempty"`
	Status        string    `json:"status,omitempty"`
	DurationMS    int64     `json:"duration_ms,omitempty"`
	Error         string    `json:"error,omitempty"`
	Attributes    Attrs     `json:"attributes,omitempty"`
}

// Sink 只负责持久化单条记录。写入错误不能反向改变 workflow 成败，由 Recorder 旁路报告。
type Sink interface {
	Write(Record) error
}

// Recorder 负责 trace identity、父子关系和故障隔离，存储实现留给 Sink。
type Recorder struct {
	sink    Sink
	onError func(error)
}

// New 用任意 Sink 创建 Recorder；onError 可为空，此时 Observation 写入失败会被安静忽略。
func New(sink Sink, onError func(error)) *Recorder {
	return &Recorder{sink: sink, onError: onError}
}

// NewJSONL 创建 story-emerge V1 使用的本地追加式后端。
func NewJSONL(path string, onError func(error)) *Recorder {
	return New(&JSONLSink{path: filepath.Clean(path)}, onError)
}

type traceContext struct {
	executionID string
	operationID string
}

type traceContextKey struct{}

// Execution 表示一次顶层运行，例如 initialize 或 continue；业务名称只用于诊断，不参与 Recorder 行为。
type Execution struct {
	recorder *Recorder
	id       string
	name     string
	started  time.Time
	once     sync.Once
}

// Operation 表示一个有持续时间的子操作。workflow 改名或新增阶段时仍复用同一个基础类型。
type Operation struct {
	recorder    *Recorder
	executionID string
	id          string
	parentID    string
	name        string
	kind        Kind
	started     time.Time
	once        sync.Once
}

// StartExecution 创建新的 trace 根，并通过 context 自动把后续 Operation 关联到本次运行。
func (recorder *Recorder) StartExecution(ctx context.Context, name string, attrs Attrs) (context.Context, *Execution) {
	if recorder == nil {
		return ctx, &Execution{}
	}
	// started 保留 monotonic clock reading 只用于耗时；写入记录时再转 UTC wall clock。
	started := time.Now()
	id := newID()
	recorder.record(Record{
		At: started.UTC(), Type: "execution_start", ExecutionID: id,
		Name: name, Kind: KindWorkflow, Attributes: attrs,
	})
	ctx = context.WithValue(ctx, traceContextKey{}, traceContext{executionID: id})
	return ctx, &Execution{recorder: recorder, id: id, name: name, started: started}
}

// End 只关闭一次 Execution；err 必须是最终 workflow 结果，不能用“某次模型请求成功”代替。
func (execution *Execution) End(err error, attrs Attrs) {
	if execution == nil || execution.recorder == nil {
		return
	}
	execution.once.Do(func() {
		finished := time.Now()
		record := Record{
			At: finished.UTC(), Type: "execution_end", ExecutionID: execution.id,
			Name: execution.name, Status: statusOf(err), DurationMS: finished.Sub(execution.started).Milliseconds(), Attributes: attrs,
		}
		if err != nil {
			record.Error = err.Error()
		}
		execution.recorder.record(record)
	})
}

// StartOperation 在 ctx 携带的 Execution/Operation 下创建有持续时间的子步骤。
func (recorder *Recorder) StartOperation(ctx context.Context, name string, kind Kind, attrs Attrs) (context.Context, *Operation) {
	if recorder == nil {
		return ctx, &Operation{}
	}
	parent, _ := ctx.Value(traceContextKey{}).(traceContext)
	// 同 Execution 一样保留 monotonic 部分用于 duration，避免 wall clock 调整影响耗时。
	started := time.Now()
	id := newID()
	recorder.record(Record{
		At: started.UTC(), Type: "operation_start", ExecutionID: parent.executionID,
		OperationID: id, ParentID: parent.operationID, Name: name, Kind: kind, Attributes: attrs,
	})
	ctx = context.WithValue(ctx, traceContextKey{}, traceContext{executionID: parent.executionID, operationID: id})
	return ctx, &Operation{
		recorder: recorder, executionID: parent.executionID, id: id, parentID: parent.operationID,
		name: name, kind: kind, started: started,
	}
}

// End 只关闭一次 Operation，并把本步骤最终结果属性追加到结束记录。
func (operation *Operation) End(err error, attrs Attrs) {
	if operation == nil || operation.recorder == nil {
		return
	}
	operation.once.Do(func() {
		finished := time.Now()
		record := Record{
			At: finished.UTC(), Type: "operation_end", ExecutionID: operation.executionID,
			OperationID: operation.id, ParentID: operation.parentID, Name: operation.name, Kind: operation.kind,
			Status: statusOf(err), DurationMS: finished.Sub(operation.started).Milliseconds(), Attributes: attrs,
		}
		if err != nil {
			record.Error = err.Error()
		}
		operation.recorder.record(record)
	})
}

// Event 记录瞬时事实；有当前 Operation 时挂在它下面，否则直接挂在 Execution 下。
func (recorder *Recorder) Event(ctx context.Context, name string, attrs Attrs) {
	if recorder == nil {
		return
	}
	parent, _ := ctx.Value(traceContextKey{}).(traceContext)
	recorder.record(Record{
		At: time.Now().UTC(), Type: "event", ExecutionID: parent.executionID,
		ParentID: parent.operationID, Name: name, Attributes: attrs,
	})
}

func (recorder *Recorder) record(record Record) {
	if recorder == nil || recorder.sink == nil {
		return
	}
	record.SchemaVersion = schemaVersion
	if err := recorder.sink.Write(record); err != nil && recorder.onError != nil {
		recorder.onError(err)
	}
}

func statusOf(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

var fallbackID atomic.Uint64

func newID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), fallbackID.Add(1))
}

// JSONLSink 每行追加一个完整 JSON 对象；互斥锁避免未来并行 workflow 分支把两条记录写到同一行。
type JSONLSink struct {
	path string
	mu   sync.Mutex
}

// Write 每次独立打开并追加文件，不让长生命周期文件描述符跨越多轮生成。
func (sink *JSONLSink) Write(record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("序列化 observation 失败: %w", err)
	}
	data = append(data, '\n')

	sink.mu.Lock()
	defer sink.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(sink.path), 0o755); err != nil {
		return fmt.Errorf("创建 observation 目录失败: %w", err)
	}
	file, err := os.OpenFile(sink.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 observation 文件失败: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入 observation 失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭 observation 文件失败: %w", err)
	}
	return nil
}
