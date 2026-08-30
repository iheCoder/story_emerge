package workflow

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"story_emerge/internal/story"
)

// deterministicIssues 检查标题、可见字数和元文本标记等确定性硬规则，
// 捕获无需再花模型费用就能确定的格式和长度错误。
func deterministicIssues(chapter string, minChars, maxChars int) []story.Issue {
	// 先做无需模型判断的硬检查，节省编辑器调用并给修订提供稳定、可重复的证据。
	// 字数使用可见 Unicode rune，而不是字节数，避免中文 UTF-8 被错误放大。

	// 检查标准章节标题。
	var issues []story.Issue
	trimmed := strings.TrimSpace(chapter)
	if !strings.HasPrefix(trimmed, "# 第") {
		issues = append(issues, hardIssue("chapter_title", "缺少标准章节标题", "正文第一行", "使用 # 第N章 标题"))
	}

	// 检查可见字数是否落在审核窗口。
	count := visibleRuneCount(trimmed)
	if count < minChars {
		issues = append(issues, hardIssue("too_short", fmt.Sprintf("正文仅 %d 字，少于 %d", count, minChars), "全文", "补足行动、阻碍和后果"))
	}
	if count > maxChars {
		issues = append(issues, hardIssue("too_long", fmt.Sprintf("正文 %d 字，超过 %d", count, maxChars), "全文", "删除重复说明并收紧场景"))
	}

	// 检查常见元文本标记，防止把写作说明输出给读者。
	for _, marker := range []string{"作为AI", "作为 AI", "本章将", "```"} {
		if strings.Contains(chapter, marker) {
			issues = append(issues, hardIssue("meta_text", "出现非小说元文本", marker, "删除解释、预告或代码围栏"))
		}
	}
	return issues
}

// visibleRuneCount 统计非空白 Unicode rune，作为中文正文的可见字符近似值。
func visibleRuneCount(text string) int {
	// 按 rune 解码并跳过空白，近似读者看到的有效字数；非法 UTF-8 也会被 DecodeRune 安全消费。
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

// hardIssue 创建一条会阻止章节提交的标准化问题。
func hardIssue(code, description, evidence, suggestion string) story.Issue {
	// 统一构造硬伤，保证 Code/Evidence/Suggestion 在确定性检查和终章检查中保持同样语义。
	return story.Issue{Code: code, Description: description, Evidence: evidence, Suggestion: suggestion}
}

// terminalClosureIssues 防止“章级好看但全书没结局”的假完成，
// 在终章检查是否仍有未 resolved 的剧情线。
func terminalClosureIssues(project story.Project, state story.State) []story.Issue {
	// 只在目标终章触发全局收束检查；中间章节允许保留悬念，但终章必须关闭所有已登记剧情线。

	// 判断当前快照是否已经到达目标终章。
	if state.Chapter != project.TargetChapters {
		return nil
	}

	// 收集仍未 resolved 的剧情线。
	var open []string
	for _, thread := range state.Threads {
		if thread.Status != "resolved" {
			open = append(open, thread.ID)
		}
	}

	// 全部收束时不产生硬伤，否则返回一条可执行的终章修订意见。
	if len(open) == 0 {
		return nil
	}
	description := fmt.Sprintf("终章仍有 %d 条剧情线未收束", len(open))
	suggestion := "把既有矛盾落实为正文中的公开结果、人物选择与可见后果，并由书记员将这些剧情线标记为 resolved；终章不得再开新悬念"
	return []story.Issue{hardIssue("terminal_threads_open", description, strings.Join(open, ", "), suggestion)}
}

// mergeDeterministicIssues 将程序硬检合并进模型审核，并保证硬伤拥有否决权。
func mergeDeterministicIssues(review story.Review, issues []story.Issue) story.Review {
	// 模型编辑意见与程序硬检查合并，程序检查拥有否决权；这样模型不能用高分掩盖长度、标题等硬违规。

	// 追加程序硬伤，并在必要时压低模型过高评分。
	if len(issues) > 0 {
		review.HardIssues = append(review.HardIssues, issues...)
		if review.Score > 60 {
			review.Score = 60
		}
	}

	// 根据合并后的硬伤和分数重算通过状态与修订动作。
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

// hardIssueInstructions 提取硬伤建议作为下一轮修订模型的最小行动列表。
func hardIssueInstructions(issues []story.Issue) []string {
	// 修订只传递硬伤的动作建议，避免把大量诊断文字再次塞进上下文导致模型忽略优先级。
	instructions := make([]string, 0, len(issues))
	for _, issue := range issues {
		instructions = append(instructions, issue.Suggestion)
	}
	return instructions
}
