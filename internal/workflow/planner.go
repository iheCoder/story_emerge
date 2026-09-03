package workflow

import (
	"context"
	"fmt"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
	"strings"
)

// planChapter 依据已提交事实和上一轮反馈提出章节意图；返回计划仍是待验收的候选方案。
func (engine *Engine) planChapter(ctx context.Context, input plannerInput, attempt int) (story.ChapterPlan, error) {
	// 每次规划独立携带完整输入，避免依赖模型会话中不可恢复的隐式记忆。
	text, err := asPrettyJSON(input)
	if err != nil {
		return story.ChapterPlan{}, err
	}

	// 尝试编号同时用于日志和工作文件，重规划时能区分每一版意图及其生成结果。
	stage := chapterStage(input.NextChapter, "plan", attempt)
	plan, err := generateJSON[story.ChapterPlan](ctx, engine, stage, llm.RolePlanner, "planner", "chapter_plan", text)
	if err != nil {
		return plan, err
	}

	// 保留模型给出的候选，再校验结构；规划成功不代表方向已提交或正文已通过。
	if err := engine.store.SaveWorking(input.NextChapter, stage+".json", plan); err != nil {
		return plan, err
	}
	return plan, story.ValidatePlan(plan)
}

// replanningFeedback 把失败意图与具体评审意见一起交还 Planner，避免只收到泛化的“重写”。
// REVISE 走到这里表示两次修订已经耗尽；REPLAN 则是 Editor 直接要求重新考虑意图。
func replanningFeedback(intent story.ChapterIntent, decision story.EditorDecision) string {
	feedback := fmt.Sprintf("被退回的章节意图：%s\n原时机理由：%s\n原约束：%s\n退回原因：%s\n需重新考虑：%s",
		intent.IntendedEffect, intent.WhyNow, strings.Join(intent.Constraints, "；"),
		decision.Reason, strings.Join(decision.BlockingIssues, "\n"))
	if decision.Action == story.EditorRevise {
		feedback = "同一意图已经修订两次，仍未通过验收。请重新考虑规划。\n" + feedback
	}
	return feedback
}
