package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// directorInput 只包含 Story Director 进行阶段判断所需的正式历史。
// 这个预备组件尚未接入章节循环，因此输入类型与 Planner/Writer/Editor 完全独立，避免现在就改变它们的权限边界。
type directorInput struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
	StoryProgress     lengthProgress          `json:"story_progress"`
	EditorEscalation  string                  `json:"editor_escalation,omitempty"`
}

// reviewStoryDirection 执行一次可独立测试和审计的 Story Director 调用。
// nextChapter 只用于日志与 .work 文件归档，不进入模型输入，也不暗示 Director 应规划该章的具体事件。
// 当前没有生产调用方；后续接线时再决定触发时机、正式 Direction 的提交边界以及与 Writer/Editor 的交互。
func (engine *Engine) reviewStoryDirection(ctx context.Context, nextChapter int, input directorInput) (story.DirectorDecision, error) {
	if nextChapter < 1 {
		return story.DirectorDecision{}, fmt.Errorf("Story Director 归档章节必须大于 0")
	}

	text, err := asPrettyJSON(input)
	if err != nil {
		return story.DirectorDecision{}, err
	}

	stage := chapterStage(nextChapter, "director")
	decision, err := generateJSON[story.DirectorDecision](ctx, engine, stage, llm.RoleDirector, "director", "director_decision", text)
	if err != nil {
		return decision, err
	}

	// 与其他结构化角色一致，先保存模型原始业务决定，再校验 KEEP/ADJUST/REPLACE 的领域语义。
	// 该文件仍位于 .work；独立调用 Director 不会更新检查点、HEAD 或任何生产 Direction。
	if err := engine.store.SaveWorking(nextChapter, stage+".json", decision); err != nil {
		return decision, err
	}
	return decision, story.ValidateDirectorDecision(input.CurrentDirection, decision)
}
