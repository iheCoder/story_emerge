package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"story_emerge/internal/story"
)

func TestCommitChapterMovesHEADOnlyAfterArtifactsExist(t *testing.T) {
	t.Parallel()
	files := New(filepath.Join(t.TempDir(), "novel"))
	project, genesis := sampleProject()
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}
	state := story.NewInitialState(genesis.InitialState)
	delta := sampleDelta()
	next, err := story.ApplyDelta(state, delta)
	if err != nil {
		t.Fatal(err)
	}
	plan := story.ChapterPlan{Number: 1, Title: "开工"}
	review := story.Review{Passed: true, Score: 80}
	if err := files.CommitChapter(plan, "# 第1章 开工\n\n正文\n", delta, review, next); err != nil {
		t.Fatal(err)
	}
	assertCommittedFiles(t, files.root)
}

func TestCommitChapterRejectsMismatchedPlanWithoutMovingHEAD(t *testing.T) {
	t.Parallel()
	files := New(filepath.Join(t.TempDir(), "novel"))
	project, genesis := sampleProject()
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}
	state := story.NewInitialState(genesis.InitialState)
	err := files.CommitChapter(story.ChapterPlan{Number: 2}, "正文", story.StateDelta{}, story.Review{}, state)
	if err == nil {
		t.Fatal("expected mismatched chapter to fail")
	}
	head, _ := os.ReadFile(filepath.Join(files.root, "HEAD"))
	if string(head) != "000\n" {
		t.Fatalf("HEAD moved unexpectedly: %q", head)
	}
}

func TestExportManuscriptUsesCommittedHEADOnly(t *testing.T) {
	t.Parallel()
	files := New(filepath.Join(t.TempDir(), "novel"))
	project, genesis := sampleProject()
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}
	state := story.NewInitialState(genesis.InitialState)
	delta := sampleDelta()
	next, err := story.ApplyDelta(state, delta)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.CommitChapter(story.ChapterPlan{Number: 1}, "# 第1章 开工\n\n正文\n", delta, story.Review{Passed: true}, next); err != nil {
		t.Fatal(err)
	}
	if err := files.SaveWorking(2, "draft.md", "不应导出的草稿"); err != nil {
		t.Fatal(err)
	}
	path, err := files.ExportManuscript()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# 测试小说\n\n# 第1章 开工\n\n正文\n\n" {
		t.Fatalf("导出稿只应包含 HEAD 历史，实际为 %q", content)
	}
}

func assertCommittedFiles(t *testing.T, root string) {
	t.Helper()
	for _, relative := range []string{
		"HEAD", "chapters/001.md", "plans/001.json", "deltas/001.json",
		"reviews/001.json", "checkpoints/001.json", "status.md",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Fatalf("missing committed file %s: %v", relative, err)
		}
	}
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "001\n" {
		t.Fatalf("unexpected HEAD: %q", head)
	}
}

func sampleProject() (story.Project, story.Genesis) {
	project := story.Project{
		Version: 1, Name: "demo", Idea: "idea", Provider: "deepseek", Model: "flash",
		TargetChapters: 3, ChapterMinChars: 500, ChapterMaxChars: 800,
		MaxCalls: 10, CreatedAt: time.Unix(0, 0).UTC(),
	}
	genesis := story.Genesis{
		Bible: story.StoryBible{
			Title: "测试小说", Genre: "都市", Logline: "一句话", ReaderPromise: "逆袭", Ending: "成功",
			ProtagonistID: "hero", Style: story.StyleGuide{},
			Characters: []story.Character{{ID: "hero", Name: "顾川", Role: "主角"}},
		},
		InitialState: story.InitialState{
			Characters: []story.CharacterState{{CharacterID: "hero", Knowledge: []story.Knowledge{}, Relationships: []story.Relationship{}}},
			Facts:      []story.Fact{},
			Threads: []story.PlotThreadState{{
				ID: "main", Name: "翻身", Kind: "main", Status: "active", PlannedPayoffChapter: 3,
			}},
			DirectorGuidance: []string{},
		},
	}
	return project, genesis
}

func sampleDelta() story.StateDelta {
	return story.StateDelta{
		Chapter:  1,
		Summary:  story.ChapterSummary{Number: 1, Title: "开工", Summary: "开始", KeyChanges: []string{"开始"}},
		NewFacts: []story.Fact{}, CharacterStates: []story.CharacterState{},
		ThreadStates: []story.PlotThreadState{}, NewThreads: []story.PlotThreadState{},
		TimelineEvents: []story.TimelineEvent{},
	}
}
