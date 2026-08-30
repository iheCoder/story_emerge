package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"story_emerge/internal/story"
)

func TestCommitChapterMovesHEADOnlyAfterArtifactsExist(t *testing.T) {
	// 场景：从空项目创建第 1 章并提交完整 plan/chapter/delta/review/state。
	// 预期：所有审计产物存在且 HEAD 从 000 移到 001，验证提交协议的正常路径。

	// 创建空项目并写入第 0 章初始快照。
	t.Parallel()
	files := New(filepath.Join(t.TempDir(), "novel"))
	project, genesis := sampleProject()
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}
	state := story.NewInitialState(genesis.InitialState)

	// 构造合法第 1 章候选状态。
	delta := sampleDelta()
	next, err := story.ApplyDelta(state, delta)
	if err != nil {
		t.Fatal(err)
	}
	plan := story.ChapterPlan{Number: 1, Title: "开工"}
	review := story.Review{Passed: true, Score: 80}

	// 提交所有产物并检查 HEAD/文件集合。
	if err := files.CommitChapter(plan, "# 第1章 开工\n\n正文\n", delta, review, next); err != nil {
		t.Fatal(err)
	}
	assertCommittedFiles(t, files.root)
}

func TestCommitChapterRejectsMismatchedPlanWithoutMovingHEAD(t *testing.T) {
	// 场景：当前状态仍是第 0 章，但调用者误传第 2 章计划。
	// 预期：CommitChapter 立即拒绝且 HEAD 保持 000，验证章节编号是提交前置条件。

	// 创建仅包含第 0 章的项目。
	t.Parallel()
	files := New(filepath.Join(t.TempDir(), "novel"))
	project, genesis := sampleProject()
	if err := files.Create(project, genesis); err != nil {
		t.Fatal(err)
	}
	state := story.NewInitialState(genesis.InitialState)

	// 提交不匹配的章节计划。
	err := files.CommitChapter(story.ChapterPlan{Number: 2}, "正文", story.StateDelta{}, story.Review{}, state)
	if err == nil {
		t.Fatal("expected mismatched chapter to fail")
	}

	// 确认拒绝后提交指针仍停留在 000。
	head, _ := os.ReadFile(filepath.Join(files.root, "HEAD"))
	if string(head) != "000\n" {
		t.Fatalf("HEAD moved unexpectedly: %q", head)
	}
}

func TestExportManuscriptUsesCommittedHEADOnly(t *testing.T) {
	// 场景：第 1 章已提交，同时在 .work 写入第 2 章草稿。
	// 预期：导出稿只包含 HEAD 历史中的第 1 章，验证未审核现场不会污染读者正文。

	// 创建并提交一章正式正文。
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

	// 故意写入未提交草稿，模拟失败现场。
	if err := files.SaveWorking(2, "draft.md", "不应导出的草稿"); err != nil {
		t.Fatal(err)
	}

	// 导出并验证只包含 HEAD 历史。
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
	// 检查一次提交必须形成的文件集合和指针值；缺任何一项都意味着恢复链不完整。
	t.Helper()

	// 确认正式章节、审计文件和状态视图全部存在。
	for _, relative := range []string{
		"HEAD", "chapters/001.md", "plans/001.json", "deltas/001.json",
		"reviews/001.json", "checkpoints/001.json", "status.md",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Fatalf("missing committed file %s: %v", relative, err)
		}
	}

	// 确认 HEAD 只推进到刚提交的章节。
	head, _ := os.ReadFile(filepath.Join(root, "HEAD"))
	if string(head) != "001\n" {
		t.Fatalf("unexpected HEAD: %q", head)
	}
}

func sampleProject() (story.Project, story.Genesis) {
	// 提供满足 Create/ApplyDelta 最小契约的固定夹具，测试只关注存储行为而非模型内容。
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
	// 构造一个最小的第 1 章空增量，便于提交测试验证文件协议而不引入额外剧情。
	return story.StateDelta{
		Chapter:  1,
		Summary:  story.ChapterSummary{Number: 1, Title: "开工", Summary: "开始", KeyChanges: []string{"开始"}},
		NewFacts: []story.Fact{}, CharacterStates: []story.CharacterState{},
		ThreadStates: []story.PlotThreadState{}, NewThreads: []story.PlotThreadState{},
		TimelineEvents: []story.TimelineEvent{},
	}
}
