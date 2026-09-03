package workflow

import (
	"context"
	"fmt"
	"strconv"

	"story_emerge/internal/story"
)

// Run 只在完整章节之间更新运行状态。limit 是本次运行的章节上限；全书停止由 Editor
// 对已接受正文的完结判断决定，不根据字数、章节数量或某个固定阶段自动结束。
func (engine *Engine) Run(ctx context.Context, limit int) (err error) {
	engine.emit("run", "开始续写")
	defer func() { engine.logOutcome("run", err) }()

	if limit < 0 {
		return fmt.Errorf("章节上限不能为负数")
	}

	// 从正式状态恢复进度；草稿和评审文件都不能直接决定已完成章数或全书是否完结。
	project, err := engine.store.LoadProject()
	if err != nil {
		return err
	}
	core, err := engine.store.LoadCore()
	if err != nil {
		return err
	}
	current, err := engine.store.LoadState()
	if err != nil {
		return err
	}

	// limit 为 0 时持续到完结；一次只计入完整提交的一章，中断章节仍需走完整的生成与验收流程。
	// 任一步失败立即返回，下一次 Run 再根据持久化状态决定从哪里继续。
	for count := 0; !current.Completed && (limit == 0 || count < limit); count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err = engine.runChapter(ctx, project, core, current)
		if err != nil {
			return err
		}
	}

	if current.Completed {
		engine.emit("complete", "故事已经完整收束")
	}
	return nil
}

// runChapter 从最后一份正式历史生成下一章；中断后再次进入也从规划开始。
// 主流程只负责规划尝试之间的协调，草稿修订和正式提交分别由专门方法完成。
func (engine *Engine) runChapter(ctx context.Context, project story.Project, core story.StoryCore, current story.State) (story.State, error) {
	// 固定本章依赖的正式事实。后续重规划只能更新候选方向和反馈，不能把失败草稿混入历史。
	base, err := engine.chapterPlanningContext(project, core, current)
	if err != nil {
		return current, err
	}

	// Editor 要求重规划时继续尝试；整个项目的调用预算提供停止边界，不因尝试次数自动接受。
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return current, err
		}
		plan, err := engine.planChapter(ctx, base, attempt)
		if err != nil {
			return current, err
		}

		// KEEP 沿用上一次候选方向，UPDATE 使用本次规划结果；此时还不更新正式检查点。
		direction := base.CurrentDirection
		if plan.CurrentDirection != nil {
			direction = *plan.CurrentDirection
		}
		chapter, decision, err := engine.writeAndReviewDraft(ctx, base, direction, plan.ChapterIntent, attempt)
		if err != nil {
			return current, err
		}

		// 只有 ACCEPT 正文能进入提取和正式提交；任何技术失败都向上返回，等待用户继续生长。
		if decision.Action == story.EditorAccept {
			return engine.commitReviewedChapter(ctx, current, plan, direction, chapter, decision)
		}

		// REPLAN 或修订耗尽都回到规划。保留候选方向，并明确告诉 Planner 上一份意图为何失败。
		base.CurrentDirection = direction
		base.PlanningFeedback = replanningFeedback(plan.ChapterIntent, decision)
	}
}

// chapterStage 把章号、操作与尝试编号编码到工作文件名和日志阶段名，方便对应同一次调用。
func chapterStage(number int, name string, attempts ...int) string {
	stage := fmt.Sprintf("chapter_%03d_%s", number, name)
	for _, attempt := range attempts {
		stage += "_" + strconv.Itoa(attempt)
	}
	return stage
}
