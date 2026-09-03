package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"strings"
)

// writeDraft 将已确定的章节意图写成初稿；只接收 Writer 专用上下文，其中不含 User Idea。
func (engine *Engine) writeDraft(ctx context.Context, input writerContext, attempt int) (string, error) {
	// 序列化专用类型，防止后续扩展 Planner 或 Editor 上下文时意外扩大 Writer 可见范围。
	text, err := asPrettyJSON(input)
	if err != nil {
		return "", err
	}

	// 初稿使用较高温度保留创作空间；生成后只归一化标题格式，不在代码里改写情节。
	chapter, err := generateText(ctx, engine, chapterStage(input.NextChapter, "write", attempt), llm.RoleWriter, "writer", text, 0.95)
	return normalizeChapterHeading(chapter), err
}

// 修订只携带相同的 Writer 白名单、当前草稿与阻断问题，不泄露 Editor 的完整输入。
func (engine *Engine) reviseDraft(ctx context.Context, input writerContext, chapter string, issues []string, attempt, revision int) (string, error) {
	// 只增加待改正文和阻断问题，不把 Editor 的完整上下文传给 Writer。
	text, err := asPrettyJSON(struct {
		Context        writerContext `json:"context"`
		CurrentDraft   string        `json:"current_draft"`
		BlockingIssues []string      `json:"blocking_issues"`
	}{input, chapter, issues})
	if err != nil {
		return "", err
	}

	// 修订沿用同一角色配置，温度稍低以聚焦已有问题；改完仍须重新经过 Editor 验收。
	result, err := generateText(ctx, engine, chapterStage(input.NextChapter, "revise", attempt, revision), llm.RoleWriter, "writer_revision", text, 0.80)
	return normalizeChapterHeading(result), err
}

// normalizeChapterHeading 只归一化已经存在的中文章节 Markdown 标题。
// 模型偶尔把一级标题写成二级标题；这种差异不包含文学语义，不值得整章重生成。
// 没有标题或首行不是“第…”章节名时保持原文，继续由 validateDraft 明确拒绝。
func normalizeChapterHeading(chapter string) string {
	trimmed := strings.TrimLeft(chapter, "\ufeff \t\r\n")
	line, rest, found := strings.Cut(trimmed, "\n")
	if !found {
		rest = ""
	}

	heading := strings.TrimLeft(line, "#")
	if heading == line || !strings.HasPrefix(strings.TrimSpace(heading), "第") {
		return chapter
	}

	normalized := "# " + strings.TrimSpace(heading)
	if found {
		normalized += "\n" + rest
	}
	return strings.TrimSpace(normalized) + "\n"
}

// validateDraft 只检查文件可提交性，不判断篇幅、节奏、巧合或情节质量。
func validateDraft(chapter string) error {
	trimmed := strings.TrimSpace(chapter)
	if !strings.HasPrefix(trimmed, "# 第") {
		return fmt.Errorf("正文缺少标准章节标题")
	}

	// 人物可以谈论 AI，正文也可以呈现代码；不能用全文关键词匹配判定出戏。
	// 是否混入创作说明交给 Editor 结合语境评审，此处不删改正文内容。

	return nil
}
