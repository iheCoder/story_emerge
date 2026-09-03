package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
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
