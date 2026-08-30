package store

import (
	"os"
	"path/filepath"
	"testing"

	"story_emerge/internal/story"
)

func TestCommitPersistsReaderAndOutlineBeforeHEAD(t *testing.T) {
	root := filepath.Join(t.TempDir(), "book")
	files := New(root)
	outline := story.StoryOutline{Version: 0, CoreConflict: "冲突", CurrentMovementID: "m1", Movements: []story.StoryMovement{{ID: "m1"}}}
	reader := story.ReaderState{Chapter: 0, SuggestedAction: "continue"}
	genesis := story.Genesis{Bible: story.StoryBible{Title: "书"}, Outline: outline, InitialReaderState: reader}
	project := story.Project{Idea: "点子", LengthProfile: "long", MaxCalls: 10}
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}

	delta := story.StateDelta{Chapter: 1, Summary: story.ChapterSummary{Number: 1}, OutlineProgress: story.OutlineProgress{CurrentMovementID: "m1", Status: "ongoing"}, StoryStatus: "ongoing"}
	next, err := story.ApplyDelta(story.NewInitialState(genesis.InitialState, outline), delta)
	if err != nil {
		t.Fatal(err)
	}
	nextReader := story.ReaderState{Chapter: 1, SuggestedAction: "adjust"}
	if err := files.CommitChapter("# 第1章 测试\n正文\n", delta, story.CanonReview{Passed: true}, nextReader, next, outline, false); err != nil {
		t.Fatal(err)
	}

	storedReader, err := files.LoadReaderState()
	if err != nil {
		t.Fatal(err)
	}
	if storedReader.SuggestedAction != "adjust" {
		t.Fatalf("读者状态未提交: %#v", storedReader)
	}
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "001\n" {
		t.Fatalf("HEAD=%q", head)
	}
}

func TestCommitRejectsMissingReaderCheckpointWithoutMovingHEAD(t *testing.T) {
	root := filepath.Join(t.TempDir(), "book")
	files := New(root)
	outline := story.StoryOutline{Version: 0, CoreConflict: "冲突", CurrentMovementID: "m1", Movements: []story.StoryMovement{{ID: "m1"}}}
	genesis := story.Genesis{Bible: story.StoryBible{Title: "书"}, Outline: outline, InitialReaderState: story.ReaderState{Chapter: 0, SuggestedAction: "continue"}}
	if err := files.Create(story.Project{Idea: "点子"}, genesis); err != nil {
		t.Fatal(err)
	}
	delta := story.StateDelta{Chapter: 1, Summary: story.ChapterSummary{Number: 1}, OutlineProgress: story.OutlineProgress{CurrentMovementID: "m1", Status: "ongoing"}, StoryStatus: "ongoing"}
	next, err := story.ApplyDelta(story.NewInitialState(genesis.InitialState, outline), delta)
	if err != nil {
		t.Fatal(err)
	}
	err = files.CommitChapter("# 第1章\n正文", delta, story.CanonReview{Passed: true}, story.ReaderState{}, next, outline, false)
	if err == nil {
		t.Fatal("缺失 Reader checkpoint 却允许提交")
	}
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "000\n" {
		t.Fatalf("失败提交推进了 HEAD: %q", head)
	}
}
