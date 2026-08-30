package workflow

import (
	"testing"

	"story_emerge/internal/story"
)

func TestHardIssuesRemoveOptionalRevisionSuggestions(t *testing.T) {
	t.Parallel()
	review := story.Review{
		Score:                80,
		HardIssues:           []story.Issue{{Code: "logic", Suggestion: "修正时间顺序"}},
		QualityIssues:        []story.Issue{{Code: "style", Suggestion: "增加环境细节"}},
		RevisionInstructions: []string{"增加环境细节"},
	}
	deterministic := []story.Issue{{Code: "too_long", Suggestion: "压缩正文"}}
	merged := mergeDeterministicIssues(review, deterministic)
	if len(merged.RevisionInstructions) != 2 {
		t.Fatalf("unexpected instructions: %#v", merged.RevisionInstructions)
	}
	if merged.RevisionInstructions[0] != "修正时间顺序" || merged.RevisionInstructions[1] != "压缩正文" {
		t.Fatalf("optional suggestion leaked into hard-fix pass: %#v", merged.RevisionInstructions)
	}
}

func TestMinorCorrectionRequiresOneHardIssueAndValidLength(t *testing.T) {
	project := story.Project{ChapterMinChars: 10, ChapterMaxChars: 20}
	work := chapterWork{
		chapter: "# 第1章\n一二三四五六七八九十",
		review:  story.Review{Score: 76, HardIssues: []story.Issue{{Code: "fact_typo"}}},
	}
	if !canMinorCorrect(project, work) {
		t.Fatal("篇幅合格且仅剩一个硬伤时应允许局部修正")
	}
	work.review.HardIssues = append(work.review.HardIssues, story.Issue{Code: "another"})
	if canMinorCorrect(project, work) {
		t.Fatal("多个硬伤不得进入局部修正")
	}
}

func TestTerminalClosureRejectsOpenThreads(t *testing.T) {
	project := story.Project{TargetChapters: 3}
	state := story.State{Chapter: 3, Threads: []story.PlotThreadState{
		{ID: "main", Status: "active"}, {ID: "done", Status: "resolved"},
	}}
	issues := terminalClosureIssues(project, state)
	if len(issues) != 1 || issues[0].Code != "terminal_threads_open" {
		t.Fatalf("终章存在未收束剧情线时应拒绝提交，实际为 %#v", issues)
	}
	state.Threads[0].Status = "resolved"
	if issues := terminalClosureIssues(project, state); len(issues) != 0 {
		t.Fatalf("所有剧情线已收束时不应报错，实际为 %#v", issues)
	}
}
