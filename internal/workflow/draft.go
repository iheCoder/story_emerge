package workflow

import (
	"context"

	"story_emerge/internal/story"
)

// 同一意图最多修订两次；第三稿仍要验收，不通过则交回 Planner 而不是自动接受。
const maxWriterRevisions = 2

// writeAndReviewDraft 负责一份章节意图下的初稿、评审与修订。
// 返回 ACCEPT 时正文可提交；返回 REPLAN 或耗尽修订的 REVISE 时由调用方重新规划。
// 模型或存储错误单独返回，不能把技术故障解释为 Editor 的文学判断。
func (engine *Engine) writeAndReviewDraft(ctx context.Context, base plannerInput, direction story.Direction, intent story.ChapterIntent, attempt int) (string, story.EditorDecision, error) {
	// 初稿和修订共用明确的 Writer 白名单，绝不透传包含 User Idea 的整个 Planner 输入。
	writer := writerContext{
		StoryCore: base.StoryCore, CurrentStoryState: base.CurrentStoryState, ChapterIntent: intent,
		PreviousChapter: base.PreviousChapter, TargetLength: base.LengthProgress.TargetLength, NextChapter: base.NextChapter,
	}
	chapter, err := engine.writeDraft(ctx, writer, attempt)
	if err != nil {
		return "", story.EditorDecision{}, err
	}

	for draft := 1; ; draft++ {
		// 先保存实际输出再做技术检查，使格式错误或后续评审失败仍有可诊断的原文。
		if err := engine.store.SaveWorking(base.NextChapter, chapterStage(base.NextChapter, "draft", attempt, draft)+".md", chapter); err != nil {
			return "", story.EditorDecision{}, err
		}
		if err := validateDraft(chapter); err != nil {
			return "", story.EditorDecision{}, err
		}

		// 每个版本都独立接受评审。ACCEPT 和 REPLAN 立即结束本轮，REVISE 仅在剩余额度内继续。
		decision, err := engine.reviewDraft(ctx, base, direction, intent, chapter, attempt, draft)
		if err != nil {
			return "", story.EditorDecision{}, err
		}
		if decision.Action == story.EditorAccept || decision.Action == story.EditorReplan || draft > maxWriterRevisions {
			return chapter, decision, nil
		}

		// 修订只附带当前正文与阻断问题；下一轮仍走相同的保存、技术检查与评审流程。
		chapter, err = engine.reviseDraft(ctx, writer, chapter, decision.BlockingIssues, attempt, draft)
		if err != nil {
			return "", story.EditorDecision{}, err
		}
	}
}
