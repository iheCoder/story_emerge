package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"story_emerge/internal/observe"
	"story_emerge/internal/story"
)

func TestObservationLinksRunChapterStructuredCallsAndDecision(t *testing.T) {
	fake := newFake(1)
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	records := readObservationRecords(t, files.Root())

	var runID, chapterOperationID string
	for _, record := range records {
		if record.Type == "execution_start" && record.Name == "story.run" {
			runID = record.ExecutionID
		}
	}
	if runID == "" {
		t.Fatal("缺少 story.run execution")
	}

	for _, record := range records {
		if record.ExecutionID == runID && record.Type == "operation_start" && record.Name == "chapter.generate" {
			chapterOperationID = record.OperationID
		}
	}
	if chapterOperationID == "" {
		t.Fatal("缺少 chapter.generate operation")
	}

	// Writer 是纯文本调用，直接挂在章节下；Editor/Commit 是结构化生成，模型调用必须先挂到 stable structured.generate 边界。
	structuredByStage := map[string]string{}
	var writerOperationID string
	for _, record := range records {
		if record.ExecutionID != runID || record.Type != "operation_start" {
			continue
		}
		stage, _ := record.Attributes["stage"].(string)
		if record.Name == "structured.generate" {
			if record.ParentID != chapterOperationID {
				t.Fatalf("structured.generate 没有挂在章节下: %#v", record)
			}
			structuredByStage[stage] = record.OperationID
		}
		if record.Name == "llm.generate" && stage == chapterStage(1, "write") {
			writerOperationID = record.OperationID
			if record.ParentID != chapterOperationID {
				t.Fatalf("Writer 模型调用父节点错误: %#v", record)
			}
			if record.Attributes["instructions"] == "" || !strings.Contains(record.Attributes["input"].(string), "current_direction") {
				t.Fatalf("模型 Observation 没有保留实际输入现场: %#v", record.Attributes)
			}
		}
	}
	if writerOperationID == "" || structuredByStage[chapterStage(1, "editor", 1)] == "" || structuredByStage[chapterStage(1, "commit")] == "" {
		t.Fatalf("章节逻辑边界不完整: writer=%q structured=%v", writerOperationID, structuredByStage)
	}

	var sawEditorModel, sawCommitModel, sawWriterOutput, sawDecision bool
	for _, record := range records {
		if record.ExecutionID != runID {
			continue
		}
		stage, _ := record.Attributes["stage"].(string)
		if record.Type == "operation_start" && record.Name == "llm.generate" {
			switch stage {
			case chapterStage(1, "editor", 1):
				sawEditorModel = record.ParentID == structuredByStage[stage]
			case chapterStage(1, "commit"):
				sawCommitModel = record.ParentID == structuredByStage[stage]
			}
		}
		if record.Type == "operation_end" && record.OperationID == writerOperationID {
			output, _ := record.Attributes["output"].(string)
			sawWriterOutput = strings.Contains(output, "第1章")
		}
		if record.Type == "event" && record.Name == "decision" && record.ParentID == chapterOperationID {
			if record.Attributes["name"] == "chapter_review" && record.Attributes["outcome"] == string(story.EditorAccept) {
				sawDecision = true
			}
		}
	}
	if !sawEditorModel || !sawCommitModel || !sawWriterOutput || !sawDecision {
		t.Fatalf("Observation 因果链不完整 editor=%v commit=%v writer_output=%v decision=%v", sawEditorModel, sawCommitModel, sawWriterOutput, sawDecision)
	}
}

func TestObservationKeepsFormatRepairInsideStructuredGeneration(t *testing.T) {
	// Commit 首次返回半截 JSON，第二次 format repair 返回合法结果。
	// 两次 llm.generate、validation.failed 和 logical retry 必须都属于同一个 structured.generate。
	fake := newFake(1)
	commitStage := chapterStage(1, "commit")
	fake.responses[commitStage] = `{"state_patch":`
	fake.responses[commitStage+"_format_repair"] = mustJSON(extracted())
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	records := readObservationRecords(t, files.Root())

	var runID, structuredID string
	for _, record := range records {
		if record.Type == "execution_start" && record.Name == "story.run" {
			runID = record.ExecutionID
		}
	}
	for _, record := range records {
		if record.ExecutionID != runID || record.Type != "operation_start" || record.Name != "structured.generate" {
			continue
		}
		if record.Attributes["stage"] == commitStage {
			structuredID = record.OperationID
		}
	}
	if structuredID == "" {
		t.Fatal("缺少 Commit structured.generate")
	}

	var calls, validationFailures, retries int
	for _, record := range records {
		if record.ExecutionID != runID || record.ParentID != structuredID {
			continue
		}
		switch {
		case record.Type == "operation_start" && record.Name == "llm.generate":
			calls++
		case record.Type == "event" && record.Name == "validation.failed":
			validationFailures++
		case record.Type == "event" && record.Name == "retry" && record.Attributes["strategy"] == "format_repair":
			retries++
		}
	}
	if calls != 2 || validationFailures != 1 || retries != 1 {
		t.Fatalf("format repair 因果链错误 calls=%d validation=%d retries=%d", calls, validationFailures, retries)
	}
}

func readObservationRecords(t *testing.T, root string) []observe.Record {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var records []observe.Record
	scanner := bufio.NewScanner(file)
	// V1 会保存完整模型输入/输出，测试读取器不能沿用 Scanner 默认的 64 KiB 单行上限。
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var record observe.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}
