package workflow

import (
	"context"
	"fmt"
	"strings"
)

const (
	chapterMaxOutputTokens = 12000
	writerTemperature      = 0.95
	revisionTemperature    = 0.80
)

type writerRevisionInput struct {
	Context          writerContext `json:"context"`
	CurrentDraft     string        `json:"current_draft,omitempty"`
	Guidance         string        `json:"editor_guidance"`
	InterventionKind string        `json:"intervention_kind"`
}

func (engine *Engine) writeDraft(ctx context.Context, chapterContext writerContext) (string, error) {
	input, err := asPrettyJSON(chapterContext)
	if err != nil {
		return "", err
	}

	chapter, err := generateTextWithGuidance(
		ctx, engine, chapterStage(chapterContext.NextChapter, "write"), "writer", input,
		engine.options.WriterGuidance, chapterMaxOutputTokens, writerTemperature,
	)
	return normalizeChapterHeading(chapter), err
}

func (engine *Engine) reviseDraft(ctx context.Context, work chapterWork, guidance string) (string, error) {
	input, err := asPrettyJSON(writerRevisionInput{
		Context: work.context, CurrentDraft: work.chapter,
		Guidance: guidance, InterventionKind: "revise",
	})
	if err != nil {
		return "", err
	}

	stage := chapterStage(work.context.NextChapter, fmt.Sprintf("revise_%d", work.reviewLog.Interventions))
	chapter, err := generateTextWithGuidance(
		ctx, engine, stage, "writer_revision", input,
		engine.options.WriterGuidance, chapterMaxOutputTokens, revisionTemperature,
	)
	return normalizeChapterHeading(chapter), err
}

func (engine *Engine) rewriteAfterReplan(ctx context.Context, work chapterWork, guidance string) (string, error) {
	input, err := asPrettyJSON(writerRevisionInput{
		Context: work.context, Guidance: guidance, InterventionKind: "replan",
	})
	if err != nil {
		return "", err
	}

	stage := chapterStage(work.context.NextChapter, fmt.Sprintf("rewrite_after_replan_%d", work.reviewLog.Interventions))
	chapter, err := generateTextWithGuidance(
		ctx, engine, stage, "writer_revision", input,
		engine.options.WriterGuidance, chapterMaxOutputTokens, writerTemperature,
	)
	return normalizeChapterHeading(chapter), err
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

func (engine *Engine) saveDraft(number, draftNumber int, chapter string) error {
	name := fmt.Sprintf("draft-%d.md", draftNumber)
	return engine.store.SaveWorking(number, name, chapter)
}
