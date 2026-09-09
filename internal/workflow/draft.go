package workflow

import (
	"context"

	"story_emerge/internal/observe"
	"story_emerge/internal/story"
)

// writeAndReviewDraft 负责 Writer 自主生成的初稿、评审与修订。
// 没有 Planner 可以退回；结构性问题也由 Writer 基于同一正式上下文重写，直到 ACCEPT、取消或调用预算停止。
func (engine *Engine) writeAndReviewDraft(ctx context.Context, base chapterContext) (string, story.EditorDecision, error) {
	// 初稿和修订共用明确的 Writer 白名单，绝不透传 Editor 的 Ledger、篇幅进度或评审上下文。
	writer := writerContext{
		StoryCore: base.StoryCore, CurrentStoryState: base.CurrentStoryState,
		CurrentDirection: base.CurrentDirection, RecentTrajectory: base.RecentTrajectory,
		PreviousChapter: base.PreviousChapter, TargetLength: base.LengthProgress.TargetLength, NextChapter: base.NextChapter,
	}
	chapter, err := engine.writeDraft(ctx, writer)
	if err != nil {
		return "", story.EditorDecision{}, err
	}

	for draft := 1; ; draft++ {
		// 先保存实际输出再做技术检查，使格式错误或后续评审失败仍有可诊断的原文。
		if err := engine.store.SaveWorking(base.NextChapter, chapterStage(base.NextChapter, "draft", draft)+".md", chapter); err != nil {
			return "", story.EditorDecision{}, err
		}
		if err := validateDraft(chapter); err != nil {
			return "", story.EditorDecision{}, err
		}

		// 每个版本都独立接受评审。Editor 只决定 ACCEPT 或继续交给 Writer 修订。
		decision, err := engine.reviewDraft(ctx, base, chapter, draft)
		if err != nil {
			return "", story.EditorDecision{}, err
		}
		if decision.ChapterDecision == story.EditorAccept {
			return chapter, decision, nil
		}

		// REVISE_WRITER 是一次业务层 retry：decision 已经记录，retry 再明确它确实改变了后续控制流。
		engine.observer.Event(ctx, "retry", observe.Attrs{
			"scope": "workflow", "reason": "decision", "decision_name": "chapter_review",
			"outcome": string(decision.ChapterDecision), "chapter": base.NextChapter, "attempt": draft,
		})

		// 修订只附带当前正文与阻断问题；下一轮仍走相同的保存、技术检查与评审流程。
		chapter, err = engine.reviseDraft(ctx, writer, chapter, decision.BlockingIssues, draft)
		if err != nil {
			return "", story.EditorDecision{}, err
		}
	}
}
