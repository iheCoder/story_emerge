package observe

import (
	"context"
	"errors"
	"testing"
)

type memorySink struct {
	records []Record
	err     error
}

func (sink *memorySink) Write(record Record) error {
	sink.records = append(sink.records, record)
	return sink.err
}

func TestRecorderBuildsParentedTraceWithoutBusinessSchema(t *testing.T) {
	sink := &memorySink{}
	recorder := New(sink, nil)

	ctx, execution := recorder.StartExecution(context.Background(), "story.run", Attrs{"workflow_version": "test"})
	ctx, operation := recorder.StartOperation(ctx, "chapter.generate", KindWorkflow, Attrs{"chapter": 3})
	_, model := recorder.StartOperation(ctx, "llm.generate", KindModel, Attrs{"role": "writer"})
	model.End(nil, Attrs{"input_tokens": 10})
	operation.End(nil, nil)
	execution.End(nil, nil)

	if len(sink.records) != 6 {
		t.Fatalf("记录数量错误: %d", len(sink.records))
	}
	root := sink.records[0]
	chapter := sink.records[1]
	modelStart := sink.records[2]
	if root.ExecutionID == "" || chapter.ExecutionID != root.ExecutionID || modelStart.ExecutionID != root.ExecutionID {
		t.Fatalf("execution 关联错误: %#v", sink.records)
	}
	if modelStart.ParentID != chapter.OperationID {
		t.Fatalf("模型调用没有挂在章节操作下: %#v", modelStart)
	}
	if chapter.Attributes["chapter"] != 3 || modelStart.Attributes["role"] != "writer" {
		t.Fatalf("可变 workflow 信息没有保留为 attributes: %#v %#v", chapter, modelStart)
	}
}

func TestRecorderFailureIsReportedButDoesNotEscape(t *testing.T) {
	want := errors.New("disk full")
	sink := &memorySink{err: want}
	var reported error
	recorder := New(sink, func(err error) { reported = err })

	ctx, execution := recorder.StartExecution(context.Background(), "story.run", nil)
	_, operation := recorder.StartOperation(ctx, "llm.generate", KindModel, nil)
	operation.End(nil, nil)
	execution.End(nil, nil)

	if !errors.Is(reported, want) {
		t.Fatalf("Observation sink 错误没有旁路报告: %v", reported)
	}
}
