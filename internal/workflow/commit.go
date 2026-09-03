package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// extractAccepted 只获得旧事实与 ACCEPT 正文；没有 User Idea、Intent、方向或 Editor 结论，
// 防止把计划当成已经发生的事实，或用验收者的解释代替正文证据。
func (engine *Engine) extractAccepted(ctx context.Context, number int, previous story.CurrentStoryState, chapter string) (story.CommitResult, error) {
	// 显式构造提取输入，只允许“此前已成立的事实 + 这次正文实际写出的内容”成为新状态依据。
	input, err := asPrettyJSON(struct {
		PreviousCurrentStoryState story.CurrentStoryState `json:"previous_current_story_state"`
		AcceptedChapter           string                  `json:"accepted_chapter"`
	}{previous, chapter})
	if err != nil {
		return story.CommitResult{}, err
	}

	// 使用 Commit 角色的配置预算与思考强度；此方法只提取，不决定是否接受正文。
	stage := chapterStage(number, "commit")
	result, err := generateJSON[story.CommitResult](ctx, engine, stage, llm.RoleCommit, "commit", "chapter_commit", input)
	if err != nil {
		return result, err
	}

	// 先保留提取结果以便诊断，再检查领域结构；.work 文件本身不推进 HEAD。
	// 正式提交由调用方在重新核对验收恢复点之后执行。
	if err := engine.store.SaveWorking(number, stage+".json", result); err != nil {
		return result, err
	}
	return result, story.ValidateCommitResult(result)
}
