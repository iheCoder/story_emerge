package workflow

import "story_emerge/internal/story"

const recentSummaryLimit = 3

// writerContext 只提供作品身份、当前方向、长期状态和短期接续信息。
// 它没有逐章施工图，也不要求 Writer 覆盖全部 Story Track。
type writerContext struct {
	Bible                   story.StoryBible         `json:"story_bible"`
	Outline                 story.StoryOutline       `json:"active_outline"`
	State                   story.State              `json:"current_story_state"`
	PreviousChapter         string                   `json:"previous_chapter"`
	RecentSummaries         []story.ChapterSummary   `json:"recent_summaries"`
	LatestReaderObservation *story.ReaderObservation `json:"latest_reader_observation,omitempty"`
	NextChapter             int                      `json:"next_chapter"`
	LengthProfile           string                   `json:"story_scale_intent"`
}

func (engine *Engine) buildWriterContext(project story.Project, bible story.StoryBible, outline story.StoryOutline, state story.State, reader *story.ReaderObservation) (writerContext, error) {
	// 上一章全文负责动作、语气与画面的近距离接续。
	previous, err := engine.store.LoadChapter(state.Chapter)
	if err != nil {
		return writerContext{}, err
	}

	// 独立摘要只提供短期历史，不重新进入长期 Story State。
	summaries, err := engine.store.LoadSummaries()
	if err != nil {
		return writerContext{}, err
	}

	return writerContext{
		Bible: bible, Outline: outline, State: state,
		PreviousChapter: previous, RecentSummaries: recentSummaries(summaries, recentSummaryLimit),
		LatestReaderObservation: reader, NextChapter: state.Chapter + 1,
		LengthProfile: project.LengthProfile,
	}, nil
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
