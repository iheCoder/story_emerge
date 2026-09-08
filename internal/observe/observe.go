// Package observe records workflow execution facts without depending on story-specific roles or stages.
// The recorder is deliberately small: business code emits executions, operations and events; sinks decide where they live.
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

// Kind describes only durable technical semantics. Story roles and workflow stages stay in attributes so they can evolve freely.
type Kind string

const (
	KindWorkflow Kind = "workflow"
	KindModel    Kind = "model"
)

// Attrs carries dimensions that are useful for diagnosis but are not part of the stable observation schema.
type Attrs map[string]any

// Record is the append-only wire format consumed by JSONL today and other sinks later.
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

// Sink persists one observation record. The workflow never sees sink errors directly; Recorder reports them out-of-band.
type Sink interface {
	Write(Record) error
}

// Recorder owns trace identity and failure isolation while leaving persistence to a Sink.
type Recorder struct {
	sink    Sink
	onError func(error)
}

// New creates a recorder around an arbitrary sink. onError may be nil when observation failures can be silently ignored.
func New(sink Sink, onError func(error)) *Recorder {
	return &Recorder{sink: sink, onError: onError}
}

// NewJSONL creates the lightweight local backend used by story-emerge V1.
func NewJSONL(path string, onError func(error)) *Recorder {
	return New(&JSONLSink{path: filepath.Clean(path)}, onError)
}

type traceContext struct {
	executionID string
	operationID string
}

type traceContextKey struct{}

// Execution is a top-level user-visible run such as initialize or continue.
type Execution struct {
	recorder *Recorder
	id       string
	name     string
	started  time.Time
	once     sync.Once
}

// Operation is a timed child step. V1 uses it for shared model calls; future workflow steps can reuse the same primitive.
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

// StartExecution creates a new trace root and returns a context that automatically parents later operations.
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

// End closes an execution exactly once. err represents the final workflow outcome, not merely a successful model request.
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

// StartOperation creates a timed child operation under the execution/operation carried by ctx.
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

// End closes an operation exactly once and records its final result attributes.
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

// Event records an instantaneous fact under the current operation, or directly under the execution when no operation is active.
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

// JSONLSink appends one self-contained JSON object per line. A mutex keeps future parallel workflow branches from interleaving records.
type JSONLSink struct {
	path string
	mu   sync.Mutex
}

// Write appends a complete record without keeping a long-lived file descriptor across runs.
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
