package workflow

import (
	"fmt"

	"story_emerge/internal/story"
)

type chapterContext struct {
	Bible           story.StoryBible `json:"story_bible"`
	State           story.State      `json:"current_state"`
	PreviousChapter string           `json:"previous_chapter"`
	NextChapter     int              `json:"next_chapter"`
	TargetChapters  int              `json:"target_chapters"`
	CharacterRange  string           `json:"chapter_character_range"`
}

// buildChapterContext 保留全部明确事实和剧情线，但只携带最近三章摘要与上一章正文。
// 这样既不依赖长上下文碰运气，也不会用“摘要的摘要”覆盖原始事实。
func (engine *Engine) buildChapterContext(
	project story.Project,
	bible story.StoryBible,
	state story.State,
) (chapterContext, error) {
	previous, err := engine.store.LoadChapter(state.Chapter)
	if err != nil {
		return chapterContext{}, err
	}
	trimmed := state
	trimmed.Summaries = recentSummaries(state.Summaries, 3)
	return chapterContext{
		Bible: bible, State: trimmed, PreviousChapter: previous,
		NextChapter: state.Chapter + 1, TargetChapters: project.TargetChapters,
		CharacterRange: fmt.Sprintf("%d-%d 个汉字", project.ChapterMinChars, project.ChapterMaxChars),
	}, nil
}

func recentSummaries(summaries []story.ChapterSummary, limit int) []story.ChapterSummary {
	if len(summaries) <= limit {
		return append([]story.ChapterSummary(nil), summaries...)
	}
	return append([]story.ChapterSummary(nil), summaries[len(summaries)-limit:]...)
}
