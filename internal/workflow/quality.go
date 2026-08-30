package workflow

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"story_emerge/internal/story"
)

// deterministicIssues 捕获无需再花模型费用就能确定的格式和长度错误。
func deterministicIssues(chapter string, minChars, maxChars int) []story.Issue {
	var issues []story.Issue
	trimmed := strings.TrimSpace(chapter)
	if !strings.HasPrefix(trimmed, "# 第") {
		issues = append(issues, hardIssue("chapter_title", "缺少标准章节标题", "正文第一行", "使用 # 第N章 标题"))
	}
	count := visibleRuneCount(trimmed)
	if count < minChars {
		issues = append(issues, hardIssue("too_short", fmt.Sprintf("正文仅 %d 字，少于 %d", count, minChars), "全文", "补足行动、阻碍和后果"))
	}
	if count > maxChars {
		issues = append(issues, hardIssue("too_long", fmt.Sprintf("正文 %d 字，超过 %d", count, maxChars), "全文", "删除重复说明并收紧场景"))
	}
	for _, marker := range []string{"作为AI", "作为 AI", "本章将", "```"} {
		if strings.Contains(chapter, marker) {
			issues = append(issues, hardIssue("meta_text", "出现非小说元文本", marker, "删除解释、预告或代码围栏"))
		}
	}
	return issues
}

func visibleRuneCount(text string) int {
	count := 0
	for len(text) > 0 {
		runeValue, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if !unicode.IsSpace(runeValue) {
			count++
		}
	}
	return count
}

func hardIssue(code, description, evidence, suggestion string) story.Issue {
	return story.Issue{Code: code, Description: description, Evidence: evidence, Suggestion: suggestion}
}

// terminalClosureIssues 防止“章级好看但全书没结局”的假完成。
func terminalClosureIssues(project story.Project, state story.State) []story.Issue {
	if state.Chapter != project.TargetChapters {
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
	description := fmt.Sprintf("终章仍有 %d 条剧情线未收束", len(open))
	suggestion := "把既有矛盾落实为正文中的公开结果、人物选择与可见后果，并由书记员将这些剧情线标记为 resolved；终章不得再开新悬念"
	return []story.Issue{hardIssue("terminal_threads_open", description, strings.Join(open, ", "), suggestion)}
}

func mergeDeterministicIssues(review story.Review, issues []story.Issue) story.Review {
	if len(issues) > 0 {
		review.HardIssues = append(review.HardIssues, issues...)
		if review.Score > 60 {
			review.Score = 60
		}
	}
	// 只要存在硬伤，修订阶段就只能收到硬伤对应的动作。
	// 否则模型常会一边补硬伤、一边扩写可选细节，最终造成新的长度问题。
	if len(review.HardIssues) > 0 {
		review.Passed = false
		review.RevisionInstructions = hardIssueInstructions(review.HardIssues)
	} else if review.Score < 70 {
		review.Passed = false
	}
	return review
}

func hardIssueInstructions(issues []story.Issue) []string {
	instructions := make([]string, 0, len(issues))
	for _, issue := range issues {
		instructions = append(instructions, issue.Suggestion)
	}
	return instructions
}
