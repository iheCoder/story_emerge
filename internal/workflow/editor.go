package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// Editor 的输入单独构造，避免复用 Planner 输入而意外引入扩展上下文。
// 原始用户要求仅在这里与 Planner/Architect 可见，不传给 Writer。
type editorInput struct {
	UserIdea          string                  `json:"user_idea"`
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	ChapterIntent     story.ChapterIntent     `json:"chapter_intent"`
	PreviousChapter   string                  `json:"previous_chapter"`
	Draft             string                  `json:"draft"`
	LengthProgress    lengthProgress          `json:"length_progress"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
}

func (engine *Engine) reviewDraft(ctx context.Context, base plannerInput, direction story.Direction, intent story.ChapterIntent, chapter string, attempt, draft int) (story.EditorDecision, error) {
	input, err := asPrettyJSON(editorInput{
		UserIdea: base.UserIdea, StoryCore: base.StoryCore, CurrentStoryState: base.CurrentStoryState,
		CurrentDirection: direction, RecentTrajectory: base.RecentTrajectory, ChapterIntent: intent,
		PreviousChapter: base.PreviousChapter, Draft: chapter, LengthProgress: base.LengthProgress, ChapterLedger: base.ChapterLedger,
	})
	if err != nil {
		return story.EditorDecision{}, err
	}
	stage := chapterStage(base.NextChapter, "editor", attempt, draft)
	decision, err := generateJSON[story.EditorDecision](ctx, engine, stage, llm.RoleEditor, "editor", "editor_decision", input, 6000, "low")
	if err != nil {
		return decision, err
	}
	if err := engine.store.SaveWorking(base.NextChapter, stage+".json", decision); err != nil {
		return decision, err
	}
	return decision, story.ValidateEditorDecision(decision)
}
