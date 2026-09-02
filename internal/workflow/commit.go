package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// extractAccepted 只获得旧事实与 ACCEPT 正文；没有 User Idea、Intent、方向或 Editor 结论，
// 防止把计划当成已经发生的事实，或用验收者的解释代替正文证据。
func (engine *Engine) extractAccepted(ctx context.Context, number int, previous story.CurrentStoryState, chapter string) (story.CommitResult, error) {
	input, err := asPrettyJSON(struct {
		PreviousCurrentStoryState story.CurrentStoryState `json:"previous_current_story_state"`
		AcceptedChapter           string                  `json:"accepted_chapter"`
	}{previous, chapter})
	if err != nil {
		return story.CommitResult{}, err
	}
	stage := chapterStage(number, "commit")
	result, err := generateJSON[story.CommitResult](ctx, engine, stage, llm.RoleCommit, "commit", "chapter_commit", input, 10000, "low")
	if err != nil {
		return result, err
	}
	if err := engine.store.SaveWorking(number, stage+".json", result); err != nil {
		return result, err
	}
	return result, story.ValidateCommitResult(result)
}
