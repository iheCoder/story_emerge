package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
	"strings"
)

// commitInput 区分状态证据与保留状态的参照。旧状态和 ACCEPT 正文说明已经发生了什么；
// Spine、Direction 和近期轨迹帮助筛选仍需保留的当前约束，不能把规划变成事实。
// 纠正调用复用同一输入，避免首次筛选与纠正时看到的上下文不一致。
type commitInput struct {
	PreviousCurrentStoryState story.CurrentStoryState `json:"previous_current_story_state"`
	AcceptedChapter           string                  `json:"accepted_chapter"`
	StorySpine                []string                `json:"story_spine"`
	CurrentDirection          story.Direction         `json:"current_direction"`
	RecentTrajectory          []story.TrajectoryEntry `json:"recent_trajectory"`
}

// extractAccepted 维护章末当前状态，同时提取本章轨迹与摘要；不读取 User Idea 或 Editor 结论。
func (engine *Engine) extractAccepted(ctx context.Context, number int, current story.State, chapter string) (story.CommitResult, error) {
	// 使用本章开始前已提交的规划与轨迹。本章实际发展由已验收正文提供，
	// 不提前运行 Director，也不把本次尚未提取的轨迹混入历史参照。
	previous := current.Story
	evidence := commitInput{
		PreviousCurrentStoryState: previous, AcceptedChapter: chapter,
		StorySpine: current.StorySpine, CurrentDirection: current.Direction, RecentTrajectory: current.RecentTrajectory,
	}
	input, err := asPrettyJSON(evidence)
	if err != nil {
		return story.CommitResult{}, err
	}

	// 状态操作错误与 JSON 格式错误不同：只有旧事实和 ACCEPT 正文能说明应如何修正。
	// 最多给 Commit 一次带明确错误的纠正机会，仍沿用原角色、输入边界和全项目调用预算。
	stage := chapterStage(number, "commit")
	template := "commit"
	for attempt := 0; ; attempt++ {
		result, err := generateJSON[story.CommitResult](ctx, engine, stage, llm.RoleCommit, template, "chapter_commit", input)
		if err != nil {
			// 网络、预算、取消及无法解析的输出不在此处重试，避免叠加通用重试循环。
			return result, err
		}

		// 两次原始结果分别归档；后续归一化不能覆盖模型真正返回的 ID。
		if err := engine.store.SaveWorking(number, stage+".json", result); err != nil {
			return result, err
		}
		raw := result
		result.StatePatch, err = story.ResolveStatePatchIDs(previous, raw.StatePatch)
		if err == nil {
			err = story.ValidateCommitResult(result)
		}
		if err == nil {
			// 在副本上预演全部操作，把更新/删除冲突、重复 ID 和关系引用错误也纳入纠正范围。
			// 预演不写文件；正式提交仍由 Store 自行校验并原子推进 HEAD。
			_, err = story.ApplyPatch(previous, result.StatePatch)
		}
		if err == nil || attempt == 1 {
			return result, err
		}

		// 保持原始证据完整。失败候选只是待纠正的提取结果，不能成为新增事实的来源。
		input, err = asPrettyJSON(struct {
			commitInput
			RejectedExtraction story.CommitResult `json:"rejected_extraction"`
			ValidationError    string             `json:"validation_error"`
		}{evidence, raw, err.Error()})
		if err != nil {
			return story.CommitResult{}, err
		}
		stage = chapterStage(number, "commit_correction")
		template = "commit_correction"
		engine.emit(stage, "事实提取未通过状态校验，按原正文纠正一次")
	}
}

// commitReviewedChapter 将本轮 ACCEPT 正文提取为事实，并连同评审一起提交。
// 这是单次运行内的提交步骤，不提供阶段恢复；失败后下次运行从正式 HEAD 继续生成。
func (engine *Engine) commitReviewedChapter(ctx context.Context, current story.State, chapter string, review story.EditorDecision) (story.State, error) {
	// Commit 以旧事实与正文提取，状态操作错误可在本次调用内纠正一次；最终失败仍返回原状态。
	// 不把 ACCEPT 当作章节已经完成，也不从上一次失败留下的候选恢复正文。
	number := current.Chapter + 1
	result, err := engine.extractAccepted(ctx, number, current, chapter)
	if err != nil {
		return current, err
	}

	title, _, _ := strings.Cut(strings.TrimSpace(chapter), "\n")
	next, err := engine.store.CommitChapter(chapter, story.ChapterCommit{
		Chapter: number, Title: strings.TrimSpace(strings.TrimPrefix(title, "# ")),
		Review: review, Result: result,
	})
	if err != nil {
		return current, err
	}

	// 存储事务最后推进 HEAD；只有此后才播报章节完成，完结判断也随同一事务生效。
	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交", next.Chapter))
	return next, nil
}
