package workflow

import "story_emerge/internal/story"

// writerContext contains exactly what a continuing novelist needs: durable
// promises, active direction, canon, immediate prose continuity, and the
// target reader's current reaction. It contains no chapter construction plan.
type writerContext struct {
	Bible                   story.StoryBible         `json:"story_bible"`
	Outline                 story.StoryOutline       `json:"active_outline"`
	State                   story.State              `json:"canonical_state"`
	PreviousChapter         string                   `json:"previous_chapter"`
	LatestReaderObservation *story.ReaderObservation `json:"latest_reader_observation,omitempty"`
	NextChapter             int                      `json:"next_chapter"`
	LengthProfile           string                   `json:"story_scale_intent"`
}

// buildWriterContext 组装 Writer 真正需要的连续创作投影，不生成章节施工图。
// 返回值不能作为下一检查点基底；完整正典由 chapterWork.canonicalBase 独立保存。
func (engine *Engine) buildWriterContext(project story.Project, bible story.StoryBible, outline story.StoryOutline, state story.State, reader *story.ReaderObservation) (writerContext, error) {
	// 阶段一：上一章全文提供局部语气和动作接续；第 0 章由 Store 返回空文本。
	previous, err := engine.store.LoadChapter(state.Chapter)
	if err != nil {
		return writerContext{}, err
	}

	// 阶段二：完整正典仍被保留，只裁剪重复的历史摘要窗口。
	trimmed := state
	trimmed.Summaries = recentSummaries(state.Summaries, recentSummaryLimit)

	// 阶段三：显式组装全部字段，让阅读者能直接看出 Writer 拥有什么权限。
	return writerContext{Bible: bible, Outline: outline, State: trimmed,
		PreviousChapter: previous, LatestReaderObservation: reader, NextChapter: state.Chapter + 1,
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
