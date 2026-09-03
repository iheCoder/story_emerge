package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
	"strings"
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
	// 正式提交由 commitReviewedChapter 统一执行，提取失败时正式历史不变。
	if err := engine.store.SaveWorking(number, stage+".json", result); err != nil {
		return result, err
	}
	return result, story.ValidateCommitResult(result)
}

// commitReviewedChapter 将本轮 ACCEPT 正文提取为事实，并连同最终计划和评审一起提交。
// 这是单次运行内的提交步骤，不提供阶段恢复；失败后下次运行从正式 HEAD 继续生成。
func (engine *Engine) commitReviewedChapter(ctx context.Context, current story.State, plan story.ChapterPlan, direction story.Direction, chapter string, review story.EditorDecision) (story.State, error) {
	// Commit 只接收旧事实与正文。提取失败直接返回原状态，不把 ACCEPT 当作章节已经完成。
	number := current.Chapter + 1
	result, err := engine.extractAccepted(ctx, number, current.Story, chapter)
	if err != nil {
		return current, err
	}

	// 最后一次 KEEP 可能沿用了较早尝试更新的方向，必须与旧 HEAD 比较后归并到提交计划。
	// plan 是局部副本，这里的 UPDATE 不会改写之前保存的规划诊断文件。
	if direction != current.Direction {
		plan.DirectionAction = "UPDATE"
		plan.CurrentDirection = &direction
	}
	title, _, _ := strings.Cut(strings.TrimSpace(chapter), "\n")
	next, err := engine.store.CommitChapter(chapter, story.ChapterCommit{
		Chapter: number, Title: strings.TrimSpace(strings.TrimPrefix(title, "# ")),
		Plan: plan, Review: review, Result: result,
	})
	if err != nil {
		return current, err
	}

	// 存储事务最后推进 HEAD；只有此后才播报章节完成，完结判断也随同一事务生效。
	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交", next.Chapter))
	return next, nil
}
