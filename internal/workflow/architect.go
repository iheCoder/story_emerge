package workflow

import (
	"context"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

const replanMaxOutputTokens = 8000

type architectReplanInput struct {
	Bible                     story.StoryBible         `json:"story_bible"`
	CurrentOutline            story.StoryOutline       `json:"current_outline"`
	State                     story.State              `json:"current_story_state"`
	RecentSummaries           []story.ChapterSummary   `json:"recent_summaries"`
	EditorGuidance            string                   `json:"editor_replan_guidance"`
	PreviousReaderObservation *story.ReaderObservation `json:"previous_reader_observation,omitempty"`
}

// replanOutline 是 Architect 的按需模式，只返回未来 Outline。
// Bible、Story Spine、已提交正文和当前 State 不在输出结构中，无法被重写。
func (engine *Engine) replanOutline(ctx context.Context, bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork, guidance string) (story.StoryOutline, error) {
	input, err := asPrettyJSON(architectReplanInput{
		Bible: bible, CurrentOutline: work.outline, State: current,
		RecentSummaries: work.context.RecentSummaries, EditorGuidance: guidance,
		PreviousReaderObservation: reader,
	})
	if err != nil {
		return story.StoryOutline{}, err
	}

	next, err := generateJSON[story.StoryOutline](ctx, engine, chapterStage(work.context.NextChapter, "architect_replan"), llm.RoleArchitect, "architect_replan", "story_outline", input, replanMaxOutputTokens, "low")
	if err != nil {
		return story.StoryOutline{}, err
	}
	next.Version = work.outline.Version + 1
	if err := story.ValidateReplan(work.outline, next); err != nil {
		return story.StoryOutline{}, err
	}

	return next, nil
}
