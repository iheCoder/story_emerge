package workflow

import (
	"fmt"

	"story_emerge/internal/story"
)

// chapterContext 是单章规划/写作/审核共享的只读输入投影。
// 它把 Bible 和完整账本保留为事实底座，同时限制摘要和上一章正文的窗口，控制上下文成本。
type chapterContext struct {
	Bible           story.StoryBible `json:"story_bible"`
	State           story.State      `json:"current_state"`
	PreviousChapter string           `json:"previous_chapter"`
	NextChapter     int              `json:"next_chapter"`
	TargetChapters  int              `json:"target_chapters"`
	CharacterRange  string           `json:"chapter_character_range"`
}

// buildChapterContext 从已提交状态构建下一章共享上下文：保留全部明确事实和剧情线，
// 但只携带最近三章摘要与上一章正文，避免依赖长上下文或“摘要的摘要”。
func (engine *Engine) buildChapterContext(
	project story.Project,
	bible story.StoryBible,
	state story.State,
) (chapterContext, error) {
	// 上一章正文必须从已提交文件读取；没有正文时返回错误，避免模型根据过时缓存续写。
	// State 只裁剪 summaries，事实、人物和剧情线全部保留，防止“摘要压缩”丢失硬约束。
	// 读取上一章已提交正文，作为连续叙事的直接上下文。
	previous, err := engine.store.LoadChapter(state.Chapter)
	if err != nil {
		return chapterContext{}, err
	}

	// 只裁剪摘要窗口，完整保留事实、人物和剧情线等硬状态。
	trimmed := state
	trimmed.Summaries = recentSummaries(state.Summaries, 3)

	// 组装下一章编号、目标章节和字数窗口。
	return chapterContext{
		Bible: bible, State: trimmed, PreviousChapter: previous,
		NextChapter: state.Chapter + 1, TargetChapters: project.TargetChapters,
		CharacterRange: fmt.Sprintf("%d-%d 个汉字", project.ChapterMinChars, project.ChapterMaxChars),
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
