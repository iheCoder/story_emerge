package workflow

import "story_emerge/internal/story"

// writerContext contains exactly what a continuing novelist needs: durable
// promises, active direction, canon, immediate prose continuity, and the
// target reader's current reaction. It contains no chapter construction plan.
type writerContext struct {
	Bible           story.StoryBible   `json:"story_bible"`
	Outline         story.StoryOutline `json:"active_outline"`
	State           story.State        `json:"canonical_state"`
	PreviousChapter string             `json:"previous_chapter"`
	ReaderState     story.ReaderState  `json:"target_reader_state"`
	NextChapter     int                `json:"next_chapter"`
	LengthProfile   string             `json:"story_scale_intent"`
}

func (engine *Engine) buildWriterContext(project story.Project, bible story.StoryBible, outline story.StoryOutline, state story.State, reader story.ReaderState) (writerContext, error) {
	previous, err := engine.store.LoadChapter(state.Chapter)
	if err != nil {
		return writerContext{}, err
	}
	trimmed := state
	trimmed.Summaries = recentSummaries(state.Summaries, recentSummaryLimit)
	return writerContext{Bible: bible, Outline: outline, State: trimmed,
		PreviousChapter: previous, ReaderState: reader, NextChapter: state.Chapter + 1,
		LengthProfile: project.LengthProfile}, nil
}

// recentSummaries 返回最近 limit 条摘要的独立副本。
func recentSummaries(summaries []story.ChapterSummary, limit int) []story.ChapterSummary {
	// 返回新切片而非原底层数组，避免上下文序列化或测试修改窗口时意外改动正式状态。
	// limit 由调用方控制为正数；即便传入更大值也安全退化为完整副本。

	// 历史不超过窗口时，复制全部摘要。
	if len(summaries) <= limit {
		return append([]story.ChapterSummary(nil), summaries...)
	}

	// 历史超出窗口时，只复制末尾最近摘要。
	return append([]story.ChapterSummary(nil), summaries[len(summaries)-limit:]...)
}
