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

// reviewDraft 评价某一版正文，返回 ACCEPT、REVISE 或 REPLAN；它本身不修改正式状态。
func (engine *Engine) reviewDraft(ctx context.Context, base plannerInput, direction story.Direction, intent story.ChapterIntent, chapter string, attempt, draft int) (story.EditorDecision, error) {
	// Editor 需要对照用户要求与历史评审正文；候选方向取本轮 Planner 的结果，而非旧方向。
	// 输入独立组装，不把规划反馈等额外信息随整个 base 一起透传。
	input, err := asPrettyJSON(editorInput{
		UserIdea: base.UserIdea, StoryCore: base.StoryCore, CurrentStoryState: base.CurrentStoryState,
		CurrentDirection: direction, RecentTrajectory: base.RecentTrajectory, ChapterIntent: intent,
		PreviousChapter: base.PreviousChapter, Draft: chapter, LengthProgress: base.LengthProgress, ChapterLedger: base.ChapterLedger,
	})
	if err != nil {
		return story.EditorDecision{}, err
	}

	// 规划尝试和草稿版本都进入阶段名，使评审结论能对应到具体正文版本。
	stage := chapterStage(base.NextChapter, "editor", attempt, draft)
	decision, err := generateJSON[story.EditorDecision](ctx, engine, stage, llm.RoleEditor, "editor", "editor_decision", input)
	if err != nil {
		return decision, err
	}

	// 单份评审文件只是诊断材料；ACCEPT 只允许本轮正文进入提取，不建立跨运行的恢复点。
	// 先落盘再做领域检查，便于定位非法动作或不完整的评审内容。
	if err := engine.store.SaveWorking(base.NextChapter, stage+".json", decision); err != nil {
		return decision, err
	}
	return decision, story.ValidateEditorDecision(decision)
}
