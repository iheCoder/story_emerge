package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// Editor 的输入单独构造。User Idea 只由 Architect 消费；Editor 根据已经建立的 Core、
// 正式历史与实际正文判断章节是否成立，不能越过 Core 重新解释原始创作授权。
type editorInput struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	RecentChapters    []string                `json:"recent_chapters"`
	Draft             string                  `json:"draft"`
	LengthProgress    lengthProgress          `json:"length_progress"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
}

// reviewDraft 评价某一版正文，返回 ACCEPT 或 REVISE_WRITER；它本身不修改正式状态。
func (engine *Engine) reviewDraft(ctx context.Context, base chapterContext, chapter string, draft int) (story.EditorDecision, error) {
	// Direction 是多章导航，不是本章必须完成的 Intent。Editor 评价本章是否形成自然贡献，
	// 并可独立请求 Commit 后的阶段复查，但不能直接修改 Direction。
	input, err := asPrettyJSON(editorInput{
		StoryCore: base.StoryCore, CurrentStoryState: base.CurrentStoryState,
		CurrentDirection: base.CurrentDirection, RecentTrajectory: base.RecentTrajectory,
		RecentChapters: base.RecentChapters, Draft: chapter, LengthProgress: base.LengthProgress, ChapterLedger: base.ChapterLedger,
	})
	if err != nil {
		return story.EditorDecision{}, err
	}

	// 规划尝试和草稿版本都进入阶段名，使评审结论能对应到具体正文版本。
	stage := chapterStage(base.NextChapter, "editor", draft)
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
