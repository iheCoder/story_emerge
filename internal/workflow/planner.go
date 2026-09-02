package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

func (engine *Engine) planChapter(ctx context.Context, input plannerInput, attempt int) (story.ChapterPlan, error) {
	text, err := asPrettyJSON(input)
	if err != nil {
		return story.ChapterPlan{}, err
	}
	stage := chapterStage(input.NextChapter, "plan", attempt)
	plan, err := generateJSON[story.ChapterPlan](ctx, engine, stage, llm.RolePlanner, "planner", "chapter_plan", text, 6000, "low")
	if err != nil {
		return plan, err
	}
	if err := engine.store.SaveWorking(input.NextChapter, stage+".json", plan); err != nil {
		return plan, err
	}
	return plan, story.ValidatePlan(plan)
}
