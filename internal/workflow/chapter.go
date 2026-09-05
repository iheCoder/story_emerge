package workflow

import (
	"context"
	"fmt"
	"strconv"

	"story_emerge/internal/story"
)

// Run 只在完整章节之间更新运行状态。limit 是本次运行的章节上限；全书停止由 Editor
// 对已接受正文的完结判断决定。每三章只触发 Direction 复查，不触发程序化完结。
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

	// 上一次运行可能已经提交章节，却在随后的 Director 调用中断。写下一章之前先补完待执行的
	// 周期复查或 Editor 请求，确保 Writer 永远使用最新正式 Direction。
	current, err = engine.reviewDirectionIfNeeded(ctx, project, core, current)
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

		// Director 必须在 ACCEPT 正文完成 Commit 之后读取最新 State、Trajectory 与 Ledger。
		// 即使本次 limit 已用完也完成这次复查，使后续运行不依赖进程内状态。
		current, err = engine.reviewDirectionIfNeeded(ctx, project, core, current)
		if err != nil {
			return err
		}
	}

	if current.Completed {
		engine.emit("complete", "故事已经完整收束")
	}
	return nil
}

// runChapter 从最后一份正式历史生成下一章。Writer 自主决定本章的局部发展，
// Editor 不通过时只退回 Writer；Planner 和 Chapter Intent 不再存在。
func (engine *Engine) runChapter(ctx context.Context, project story.Project, core story.StoryCore, current story.State) (story.State, error) {
	// 固定本章依赖的正式事实。修订可以改变正文实现，但不能把失败草稿混入历史或改写 Direction。
	base, err := engine.buildChapterContext(project, core, current)
	if err != nil {
		return current, err
	}
	chapter, decision, err := engine.writeAndReviewDraft(ctx, base)
	if err != nil {
		return current, err
	}

	// 只有 ACCEPT 正文能进入提取和正式提交；Writer 修订循环只由取消或全项目调用预算停止，
	// 预算耗尽绝不能成为自动接受一份正文的理由。
	return engine.commitReviewedChapter(ctx, current, chapter, decision)
}

// chapterStage 把章号、操作与尝试编号编码到工作文件名和日志阶段名，方便对应同一次调用。
func chapterStage(number int, name string, attempts ...int) string {
	stage := fmt.Sprintf("chapter_%03d_%s", number, name)
	for _, attempt := range attempts {
		stage += "_" + strconv.Itoa(attempt)
	}
	return stage
}
