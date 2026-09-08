package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"story_emerge/internal/observe"
)

func TestObservationLinksRunChapterAndModelCalls(t *testing.T) {
	fake := newFake(1)
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(files.Root(), "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var records []observe.Record
	scanner := bufio.NewScanner(file)
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

	var runID, chapterOperationID string
	var sawWriter, sawEditor, sawCommit bool
	for _, record := range records {
		if record.Type == "execution_start" && record.Name == "story.run" {
			runID = record.ExecutionID
		}
	}
	if runID == "" {
		t.Fatal("缺少 story.run execution")
	}

	for _, record := range records {
		if record.ExecutionID != runID {
			continue
		}
		if record.Type == "operation_start" && record.Name == "chapter.generate" {
			chapterOperationID = record.OperationID
		}
	}
	if chapterOperationID == "" {
		t.Fatal("缺少 chapter.generate operation")
	}

	for _, record := range records {
		if record.ExecutionID != runID || record.Type != "operation_start" || record.Name != "llm.generate" {
			continue
		}
		if record.ParentID != chapterOperationID {
			t.Fatalf("章节内模型调用没有挂到 chapter.generate: %#v", record)
		}
		stage, _ := record.Attributes["stage"].(string)
		switch stage {
		case chapterStage(1, "write"):
			sawWriter = true
		case chapterStage(1, "editor", 1):
			sawEditor = true
		case chapterStage(1, "commit"):
			sawCommit = true
		}
	}
	if !sawWriter || !sawEditor || !sawCommit {
		t.Fatalf("章节模型轨迹不完整 writer=%v editor=%v commit=%v", sawWriter, sawEditor, sawCommit)
	}
}
