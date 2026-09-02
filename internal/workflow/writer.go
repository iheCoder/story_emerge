package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"strings"
)

func (engine *Engine) writeDraft(ctx context.Context, input writerContext, attempt int) (string, error) {
	text, err := asPrettyJSON(input)
	if err != nil {
		return "", err
	}
	chapter, err := generateText(ctx, engine, chapterStage(input.NextChapter, "write", attempt), llm.RoleWriter, "writer", text, 12000, 0.95)
	return normalizeChapterHeading(chapter), err
}

// 修订只携带相同的 Writer 白名单、当前草稿与阻断问题，不泄露 Editor 的完整输入。
func (engine *Engine) reviseDraft(ctx context.Context, input writerContext, chapter string, issues []string, attempt, revision int) (string, error) {
	text, err := asPrettyJSON(struct {
		Context        writerContext `json:"context"`
		CurrentDraft   string        `json:"current_draft"`
		BlockingIssues []string      `json:"blocking_issues"`
	}{input, chapter, issues})
	if err != nil {
		return "", err
	}
	result, err := generateText(ctx, engine, chapterStage(input.NextChapter, "revise", attempt, revision), llm.RoleWriter, "writer_revision", text, 12000, 0.80)
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
	for _, marker := range []string{"作为AI", "作为 AI", "```"} {
		if strings.Contains(chapter, marker) {
			return fmt.Errorf("正文出现非小说元文本: %s", marker)
		}
	}

	return nil
}
