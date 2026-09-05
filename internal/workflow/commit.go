package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
	"strings"
)

// extractAccepted 以旧事实与 ACCEPT 正文为提取依据；没有 User Idea、Intent、方向或 Editor 结论，
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
			PreviousCurrentStoryState story.CurrentStoryState `json:"previous_current_story_state"`
			AcceptedChapter           string                  `json:"accepted_chapter"`
			RejectedExtraction        story.CommitResult      `json:"rejected_extraction"`
			ValidationError           string                  `json:"validation_error"`
		}{previous, chapter, raw, err.Error()})
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
	result, err := engine.extractAccepted(ctx, number, current.Story, chapter)
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
