package workflow

import (
	"testing"

	"story_emerge/internal/story"
)

func TestHardIssuesRemoveOptionalRevisionSuggestions(t *testing.T) {
	// 场景：已有一个硬伤和一个风格建议，确定性检查又发现超长。
	// 预期：修订指令只包含两个硬伤动作，验证模型不能在硬伤未修时扩写可选细节。

	// 准备模型审核结果和程序确定性硬伤。
	t.Parallel()
	review := story.Review{
		Score:                80,
		HardIssues:           []story.Issue{{Code: "logic", Suggestion: "修正时间顺序"}},
		QualityIssues:        []story.Issue{{Code: "style", Suggestion: "增加环境细节"}},
		RevisionInstructions: []string{"增加环境细节"},
	}
	deterministic := []story.Issue{{Code: "too_long", Suggestion: "压缩正文"}}

	// 合并两类审核结果。
	merged := mergeDeterministicIssues(review, deterministic)

	// 验证修订动作只包含硬伤建议。
	if len(merged.RevisionInstructions) != 2 {
		t.Fatalf("unexpected instructions: %#v", merged.RevisionInstructions)
	}
	if merged.RevisionInstructions[0] != "修正时间顺序" || merged.RevisionInstructions[1] != "压缩正文" {
		t.Fatalf("optional suggestion leaked into hard-fix pass: %#v", merged.RevisionInstructions)
	}
}

func TestMinorCorrectionRequiresOneHardIssueAndValidLength(t *testing.T) {
	// 场景：正文长度合格且评分 76，仅一个硬伤；随后追加第二个硬伤作为边界输入。
	// 预期：前者允许局部纠错，后者拒绝，验证 correct 阶段不会承担结构性重写。

	// 准备长度合格且只有一个硬伤的工作集。
	project := story.Project{ChapterMinChars: 10, ChapterMaxChars: 20}
	work := chapterWork{
		chapter: "# 第1章\n一二三四五六七八九十",
		review:  story.Review{Score: 76, HardIssues: []story.Issue{{Code: "fact_typo"}}},
	}
	// 确认单个硬伤可进入局部纠错。
	if !canMinorCorrect(project, work) {
		t.Fatal("篇幅合格且仅剩一个硬伤时应允许局部修正")
	}

	// 增加第二个硬伤，确认局部纠错门关闭。
	work.review.HardIssues = append(work.review.HardIssues, story.Issue{Code: "another"})
	if canMinorCorrect(project, work) {
		t.Fatal("多个硬伤不得进入局部修正")
	}
}

func TestTerminalClosureRejectsOpenThreads(t *testing.T) {
	// 场景：目标终章仍有 active 主线，另有一条已 resolved 线；随后把主线也标为 resolved。
	// 预期：第一次产生 terminal_threads_open，第二次无问题，验证全书收束检查只看终章且逐线生效。

	// 准备包含未收束和已收束剧情线的终章状态。
	project := story.Project{TargetChapters: 3}
	state := story.State{Chapter: 3, Threads: []story.PlotThreadState{
		{ID: "main", Status: "active"}, {ID: "done", Status: "resolved"},
	}}

	// 检查未收束分支。
	issues := terminalClosureIssues(project, state)
	if len(issues) != 1 || issues[0].Code != "terminal_threads_open" {
		t.Fatalf("终章存在未收束剧情线时应拒绝提交，实际为 %#v", issues)
	}

	// 关闭最后一条剧情线并确认不再报错。
	state.Threads[0].Status = "resolved"
	if issues := terminalClosureIssues(project, state); len(issues) != 0 {
		t.Fatalf("所有剧情线已收束时不应报错，实际为 %#v", issues)
	}
}
