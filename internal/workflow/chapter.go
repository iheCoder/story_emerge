package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"story_emerge/internal/story"
)

const maxWriterRevisions = 2

// Run 只在完整章节之间更新运行状态。limit 是本次运行的章节上限；全书停止由 Editor
// 对已接受正文的完结判断决定，不根据字数、章节数量或某个固定阶段自动结束。
func (engine *Engine) Run(ctx context.Context, limit int) (err error) {
	engine.emit("run", "开始续写")
	defer func() { engine.logOutcome("run", err) }()

	if limit < 0 {
		return fmt.Errorf("章节上限不能为负数")
	}

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

func (engine *Engine) runChapter(ctx context.Context, project story.Project, core story.StoryCore, current story.State) (story.State, error) {
	previous, err := engine.store.LoadChapter(current.Chapter)
	if err != nil {
		return current, err
	}

	ledger, err := engine.store.LoadLedger()
	if err != nil {
		return current, err
	}

	base := plannerInput{
		UserIdea: project.Idea, StoryCore: core, CurrentStoryState: current.Story,
		CurrentDirection: current.Direction, RecentTrajectory: current.RecentTrajectory,
		ChapterLedger: ledger, PreviousChapter: previous, NextChapter: current.Chapter + 1,
		LengthProgress: lengthProgress{TargetLength: story.LengthGoal(project.LengthProfile), WrittenCharacters: current.WrittenCharacters},
	}

	// 规划失败可以重新选择意图，但所有尝试仍使用同一份已提交事实。
	// 不另设文学重规划配额；整个项目的模型调用预算为此循环提供最终停止边界。
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return current, err
		}

		plan, err := engine.planChapter(ctx, base, attempt)
		if err != nil {
			return current, err
		}

		direction := base.CurrentDirection
		if plan.CurrentDirection != nil {
			direction = *plan.CurrentDirection
		}
		writer := writerContext{
			StoryCore: core, CurrentStoryState: current.Story, ChapterIntent: plan.ChapterIntent,
			PreviousChapter: previous, TargetLength: base.LengthProgress.TargetLength, NextChapter: base.NextChapter,
		}
		chapter, err := engine.writeDraft(ctx, writer, attempt)
		if err != nil {
			return current, err
		}

		// 初稿和最多两次修订都必须经过 Editor；次数耗尽也绝不自动接受。
		for draft := 1; ; draft++ {
			if err := engine.store.SaveWorking(base.NextChapter, chapterStage(base.NextChapter, "draft", attempt, draft)+".md", chapter); err != nil {
				return current, err
			}

			if err := validateDraft(chapter); err != nil {
				return current, err
			}

			decision, err := engine.reviewDraft(ctx, base, direction, plan.ChapterIntent, chapter, attempt, draft)
			if err != nil {
				return current, err
			}

			if decision.Action == story.EditorAccept {
				result, err := engine.extractAccepted(ctx, base.NextChapter, current.Story, chapter)
				if err != nil {
					return current, err
				}

				// 最终 KEEP 指的是本轮候选方向。若更早的失败规划已经更新方向，提交时必须
				// 保留这份 Planner 决定，而不能因最后一次 KEEP 回退到旧 HEAD 的方向。
				committedPlan := plan
				if direction != current.Direction {
					committedPlan.DirectionAction = "UPDATE"
					committedPlan.CurrentDirection = &direction
				}
				title, _, _ := strings.Cut(strings.TrimSpace(chapter), "\n")
				next, err := engine.store.CommitChapter(chapter, story.ChapterCommit{
					Chapter: base.NextChapter, Title: strings.TrimSpace(strings.TrimPrefix(title, "# ")),
					Plan: committedPlan, Review: decision, Result: result,
				})
				if err != nil {
					return current, err
				}

				engine.emit("chapter", fmt.Sprintf("第 %d 章已提交", next.Chapter))
				return next, nil
			}

			if decision.Action == story.EditorReplan || draft > maxWriterRevisions {
				base.CurrentDirection = direction
				// Planner 的每次调用独立，必须知道哪份意图失败，不能只收到泛化的退回意见。
				base.PlanningFeedback = fmt.Sprintf("被退回的章节意图：%s\n原时机理由：%s\n原约束：%s\n退回原因：%s\n需重新考虑：%s",
					plan.ChapterIntent.IntendedEffect, plan.ChapterIntent.WhyNow,
					strings.Join(plan.ChapterIntent.Constraints, "；"), decision.Reason, strings.Join(decision.BlockingIssues, "\n"))
				if decision.Action == story.EditorRevise {
					base.PlanningFeedback = "同一意图已经修订两次，仍未通过验收。请重新考虑规划。\n" + base.PlanningFeedback
				}
				break
			}
			chapter, err = engine.reviseDraft(ctx, writer, chapter, decision.BlockingIssues, attempt, draft)
			if err != nil {
				return current, err
			}
		}
	}
}

func chapterStage(number int, name string, attempts ...int) string {
	stage := fmt.Sprintf("chapter_%03d_%s", number, name)
	for _, attempt := range attempts {
		stage += "_" + strconv.Itoa(attempt)
	}
	return stage
}
