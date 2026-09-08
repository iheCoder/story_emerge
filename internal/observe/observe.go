// Package observe 记录工作流执行事实，不依赖小说角色、阶段或当前业务编排。
// Recorder 刻意保持很薄：业务只描述 execution、operation 和 event，具体落盘方式由 Sink 决定。
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

// Kind 只表达长期稳定的技术语义。角色、阶段等易变 workflow 信息全部放在 attributes 中。
type Kind string

const (
	KindWorkflow Kind = "workflow"
	KindModel    Kind = "model"
)

// Attrs 保存排障有价值但不属于稳定 Observation Schema 的维度。
type Attrs map[string]any

// Record 是追加式观测记录的稳定线格式；V1 写入 JSONL，后续 Sink 可投递到 OTel 等后端。
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

// Sink 只负责持久化一条 Record。写入错误由 Recorder 旁路报告，不能反向污染业务结果。
type Sink interface {
	Write(Record) error
}

// Recorder 管理 trace 身份、父子关系和 Observation 自身的故障隔离。
type Recorder struct {
	sink    Sink
	onError func(error)
}

// New 用任意 Sink 创建 Recorder。onError 可为空，此时 Observation 写入失败被静默忽略。
func New(sink Sink, onError func(error)) *Recorder {
	return &Recorder{sink: sink, onError: onError}
}

// NewJSONL 创建 V1 使用的本地追加式 JSONL 后端。
func NewJSONL(path string, onError func(error)) *Recorder {
	return New(&JSONLSink{path: filepath.Clean(path)}, onError)
}

type traceContext struct {
	executionID string
	operationID string
}

type traceContextKey struct{}

// Execution 表示一次顶层用户运行，例如初始化或继续生成。
type Execution struct {
	recorder *Recorder
	id       string
	name     string
	started  time.Time
	once     sync.Once
}

// Operation 表示一个有持续时间的子步骤。V1 用于章节和模型调用，后续 workflow 可直接复用。
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

// StartExecution 创建 trace 根节点，并把 execution 身份放进 context 供后续 Operation 自动继承。
func (recorder *Recorder) StartExecution(ctx context.Context, name string, attrs Attrs) (context.Context, *Execution) {
	if recorder == nil {
		return ctx, &Execution{}
	}
	started := time.Now().UTC()
	id := newID()
	recorder.record(Record{
		At: started, Type: "execution_start", ExecutionID: id,
		Name: name, Kind: KindWorkflow, Attributes: attrs,
	})
	ctx = context.WithValue(ctx, traceContextKey{}, traceContext{executionID: id})
	return ctx, &Execution{recorder: recorder, id: id, name: name, started: started}
}

// End 只结束一次 Execution。err 必须代表最终业务结果，不能拿某次模型请求成功冒充整轮成功。
func (execution *Execution) End(err error, attrs Attrs) {
	if execution == nil || execution.recorder == nil {
		return
	}
	execution.once.Do(func() {
		record := Record{
			At: time.Now().UTC(), Type: "execution_end", ExecutionID: execution.id,
			Name: execution.name, Status: statusOf(err), DurationMS: time.Since(execution.started).Milliseconds(), Attributes: attrs,
		}
		if err != nil {
			record.Error = err.Error()
		}
		execution.recorder.record(record)
	})
}

// StartOperation 根据 ctx 中的 trace 身份创建子步骤；业务层无需手工传递 execution_id 或 parent_id。
func (recorder *Recorder) StartOperation(ctx context.Context, name string, kind Kind, attrs Attrs) (context.Context, *Operation) {
	if recorder == nil {
		return ctx, &Operation{}
	}
	parent, _ := ctx.Value(traceContextKey{}).(traceContext)
	started := time.Now().UTC()
	id := newID()
	recorder.record(Record{
		At: started, Type: "operation_start", ExecutionID: parent.executionID,
		OperationID: id, ParentID: parent.operationID, Name: name, Kind: kind, Attributes: attrs,
	})
	ctx = context.WithValue(ctx, traceContextKey{}, traceContext{executionID: parent.executionID, operationID: id})
	return ctx, &Operation{
		recorder: recorder, executionID: parent.executionID, id: id, parentID: parent.operationID,
		name: name, kind: kind, started: started,
	}
}

// End 只结束一次 Operation，并把最终结果属性与状态写入同一个 operation_id。
func (operation *Operation) End(err error, attrs Attrs) {
	if operation == nil || operation.recorder == nil {
		return
	}
	operation.once.Do(func() {
		record := Record{
			At: time.Now().UTC(), Type: "operation_end", ExecutionID: operation.executionID,
			OperationID: operation.id, ParentID: operation.parentID, Name: operation.name, Kind: operation.kind,
			Status: statusOf(err), DurationMS: time.Since(operation.started).Milliseconds(), Attributes: attrs,
		}
		if err != nil {
			record.Error = err.Error()
		}
		operation.recorder.record(record)
	})
}

// Event 记录瞬时事实；有父 Operation 时挂在其下，否则直接挂在 Execution 下。
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

// JSONLSink 一行写一条完整 Record。互斥锁避免未来并行 workflow 分支把同一文件写成交错字节流。
type JSONLSink struct {
	path string
	mu   sync.Mutex
}

// Write 每次追加一条完整 JSON，不让 Recorder 长期持有文件句柄。
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
