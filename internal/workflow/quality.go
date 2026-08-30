package workflow

import (
	"fmt"
	"strings"

	"story_emerge/internal/story"
)

// deterministicIssues 只检查程序能够从字面证明的格式问题。
// 篇幅再短也不能证明正文被截断，因此这里没有任何字符数门槛。
func deterministicIssues(chapter string) []story.Issue {
	var issues []story.Issue
	trimmed := strings.TrimSpace(chapter)
	if !strings.HasPrefix(trimmed, "# 第") {
		issues = append(issues, hardIssue("chapter_title", "缺少标准章节标题", "正文第一行", "使用 # 第N章 标题"))
	}
	for _, marker := range []string{"作为AI", "作为 AI", "```"} {
		if strings.Contains(chapter, marker) {
			issues = append(issues, hardIssue("meta_text", "出现非小说元文本", marker, "删除模型说明或代码围栏"))
		}
	}
	return issues
}

func terminalClosureIssues(state story.State) []story.Issue {
	if state.StoryStatus != "completed" {
		return nil
	}
	var open []string
	for _, thread := range state.Threads {
		if thread.Status != "resolved" {
			open = append(open, thread.ID)
		}
	}
	if len(open) == 0 {
		return nil
	}
	return []story.Issue{hardIssue("terminal_threads_open",
		fmt.Sprintf("故事已完成但仍有 %d 条剧情线未收束", len(open)), strings.Join(open, ", "),
		"让正文给出这些矛盾的可见结果，或把 story_status 保持为 ending")}
}

func mergeDeterministicIssues(review story.CanonReview, issues []story.Issue) story.CanonReview {
	if len(issues) == 0 {
		return review
	}
	review.Issues = append(review.Issues, issues...)
	review.Passed = false
	for _, issue := range issues {
		review.RevisionInstructions = append(review.RevisionInstructions, issue.Suggestion)
	}
	return review
}

func hardIssue(code, description, evidence, suggestion string) story.Issue {
	return story.Issue{Code: code, Description: description, Evidence: evidence, Suggestion: suggestion}
}
