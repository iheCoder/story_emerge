package store

import (
	"os"
	"path/filepath"
	"testing"

	"story_emerge/internal/story"
)

func TestCommitPersistsEditorialArtifactsBeforeHEAD(t *testing.T) {
	// 场景：第一章正文、最终 Story Update、Editor 轨迹和 Reader 观察全部有效。
	// 预期：所有产物先写入各自目录，HEAD 最后才推进到 001。
	root := filepath.Join(t.TempDir(), "book")
	files, outline, genesis := createTestProject(t, root)

	update := story.StoryUpdate{Chapter: 1, StoryStatus: "ongoing"}
	next, err := story.ApplyStoryUpdate(story.NewInitialState(genesis.InitialState, outline), outline, update, []string{"旅行者是否真正愿意离开熟悉生活"})
	if err != nil {
		t.Fatal(err)
	}
	summary := story.ChapterSummary{Number: 1, Title: "启程", Summary: "旅行者离开家门"}
	review := story.EditorReviewLog{Chapter: 1, Interventions: 0}
	observation := story.ReaderObservation{Chapter: 1, Momentum: "想看旅行者走向哪里"}

	if err := files.CommitChapter("# 第1章 启程\n正文\n", update, summary, review, observation, next, outline, false); err != nil {
		t.Fatal(err)
	}

	stored, err := files.LoadLatestReaderObservation()
	if err != nil || stored == nil || stored.Momentum != "想看旅行者走向哪里" {
		t.Fatalf("Reader Observation 未提交: observation=%#v err=%v", stored, err)
	}
	summaries, err := files.LoadSummaries()
	if err != nil || len(summaries) != 1 || summaries[0].Title != "启程" {
		t.Fatalf("独立摘要未提交: summaries=%#v err=%v", summaries, err)
	}
	state, err := files.LoadState()
	if err != nil || len(state.LiveTensions) != 1 || state.LiveTensions[0] != "旅行者是否真正愿意离开熟悉生活" {
		t.Fatalf("Live Tension 未随检查点提交: state=%#v err=%v", state, err)
	}
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "001\n" {
		t.Fatalf("HEAD=%q", head)
	}
}

func TestCommitFailureDoesNotMoveHEAD(t *testing.T) {
	// 场景：Reader Observation 的章节号与候选检查点不一致。
	// 预期：提交在写入 HEAD 前失败，旧项目仍能从第 0 章恢复。
	root := filepath.Join(t.TempDir(), "book")
	files, outline, genesis := createTestProject(t, root)
	update := story.StoryUpdate{Chapter: 1, StoryStatus: "ongoing"}
	next, err := story.ApplyStoryUpdate(story.NewInitialState(genesis.InitialState, outline), outline, update, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = files.CommitChapter(
		"# 第1章\n正文", update,
		story.ChapterSummary{Number: 1, Title: "标题", Summary: "摘要"},
		story.EditorReviewLog{Chapter: 1}, story.ReaderObservation{}, next, outline, false,
	)
	if err == nil {
		t.Fatal("错误章节的 Reader Observation 却允许提交")
	}
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "000\n" {
		t.Fatalf("失败提交推进了 HEAD: %q", head)
	}
}

func TestOldStateShapeFailsInsteadOfLosingContextSilently(t *testing.T) {
	// 场景：旧项目检查点仍包含已经删除的 durable_states 与 track_progress。
	// 预期：读取明确失败，不能把未知字段静默丢弃后带着空 Situation State 继续写作。
	root := filepath.Join(t.TempDir(), "book")
	if err := os.MkdirAll(filepath.Join(root, "checkpoints"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "HEAD"), []byte("000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := `{"chapter":0,"character_states":[],"durable_states":[],"track_progress":[],"outline_version":0,"story_status":"ongoing"}`
	if err := os.WriteFile(filepath.Join(root, "checkpoints", "000.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := New(root).LoadState(); err == nil {
		t.Fatal("旧状态字段被静默忽略，可能导致缺失上下文后继续生成")
	}
}

func createTestProject(t *testing.T, root string) (*Store, story.StoryOutline, story.Genesis) {
	t.Helper()

	outline := story.StoryOutline{
		Version: 0, CurrentArc: story.StoryArc{Name: "启程", Purpose: "走出家门"},
		Tracks: []story.StoryTrack{{ID: "journey", Name: "旅程", Direction: "向山外", Status: "ongoing"}},
	}
	genesis := story.Genesis{Bible: story.StoryBible{Title: "书"}, Outline: outline}
	files := New(root)
	if err := files.Create(story.Project{Idea: "点子"}, genesis); err != nil {
		t.Fatal(err)
	}

	return files, outline, genesis
}
