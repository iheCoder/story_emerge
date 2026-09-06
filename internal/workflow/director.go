package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

const directionReviewInterval = 3

// directorInput 提供固定的创作参照、正式历史和当前方向，供 Director 判断近期发展。
// Spine 与 Direction 都是规划信息；仅事实和正式正文能证明某项变化已经发生。
type directorInput struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	StorySpine        []string                `json:"story_spine"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
	RecentChapters    []string                `json:"recent_chapters"`
	StoryProgress     lengthProgress          `json:"story_progress"`
	EditorEscalation  string                  `json:"editor_escalation,omitempty"`
}

// reviewStoryDirection 执行一次阶段复查。afterChapter 只用于日志与 .work 归档，
// 不进入模型输入，也不暗示 Director 应规划下一章的具体事件。
func (engine *Engine) reviewStoryDirection(ctx context.Context, afterChapter int, input directorInput) (story.DirectorDecision, error) {
	if afterChapter < 1 {
		return story.DirectorDecision{}, fmt.Errorf("Story Director 归档章节必须大于 0")
	}

	text, err := asPrettyJSON(input)
	if err != nil {
		return story.DirectorDecision{}, err
	}

	stage := chapterStage(afterChapter, "director")
	decision, err := generateJSON[story.DirectorDecision](ctx, engine, stage, llm.RoleDirector, "director", "director_decision", text)
	if err != nil {
		return decision, err
	}

	// 与其他结构化角色一致，先保存模型原始业务决定，再校验 KEEP/ADJUST/REPLACE 的领域语义。
	// .work 先保留模型原始决定；只有调用方随后完成正式 Direction Review 提交才会生效。
	if err := engine.store.SaveWorking(afterChapter, stage+".json", decision); err != nil {
		return decision, err
	}
	return decision, story.ValidateDirectorDecision(input.CurrentDirection, decision)
}

// reviewDirectionIfNeeded 在正式章节提交后执行周期复查或响应 Editor 的提前请求。
// 调用失败时已提交章节保持可见，Direction 仍停留在旧版本；下一次 Run 会在写新章前重试。
func (engine *Engine) reviewDirectionIfNeeded(ctx context.Context, project story.Project, core story.StoryCore, current story.State) (story.State, error) {
	if current.Completed || current.Chapter == 0 || current.DirectionReviewedAfterChapter == current.Chapter {
		return current, nil
	}

	// Editor 的请求随 ACCEPT 章节提交，Director 只读取最近一章的正式评审，不读取失败草稿意见。
	commit, err := engine.store.LoadCommit(current.Chapter)
	if err != nil {
		return current, err
	}
	escalation := ""
	if commit.Review.DirectionReview.Requested {
		escalation = commit.Review.DirectionReview.Reason
	}
	scheduled := current.Chapter%directionReviewInterval == 0
	if !scheduled && escalation == "" {
		return current, nil
	}

	ledger, err := engine.store.LoadLedger()
	if err != nil {
		return current, err
	}
	recentChapters, err := engine.loadRecentChapters(current.Chapter)
	if err != nil {
		return current, err
	}
	decision, err := engine.reviewStoryDirection(ctx, current.Chapter, directorInput{
		StoryCore: core, StorySpine: current.StorySpine, CurrentStoryState: current.Story, CurrentDirection: current.Direction,
		RecentTrajectory: current.RecentTrajectory, ChapterLedger: ledger,
		RecentChapters:   recentChapters,
		StoryProgress:    lengthProgress{TargetLength: story.LengthGoal(project.LengthProfile), WrittenCharacters: current.WrittenCharacters},
		EditorEscalation: escalation,
	})
	if err != nil {
		return current, err
	}

	version := current.DirectionVersion
	if decision.Action != story.DirectorKeep {
		version++
	}
	next, err := engine.store.CommitDirectionReview(story.DirectionReview{
		AfterChapter: current.Chapter, Version: version, Decision: decision,
	})
	if err != nil {
		return current, err
	}
	engine.emit("direction", fmt.Sprintf("第 %d 章后阶段方向已复查：%s", current.Chapter, decision.Action))
	return next, nil
}
