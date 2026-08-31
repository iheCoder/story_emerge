package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/story"
)

const editorMaxOutputTokens = 8000

type editorInput struct {
	Bible                     story.StoryBible         `json:"story_bible"`
	Outline                   story.StoryOutline       `json:"active_outline"`
	State                     story.State              `json:"current_story_state"`
	RecentSummaries           []story.ChapterSummary   `json:"recent_summaries"`
	PreviousChapter           string                   `json:"previous_chapter"`
	PreviousReaderObservation *story.ReaderObservation `json:"previous_reader_observation,omitempty"`
	CurrentDraft              string                   `json:"current_draft"`
}

func (engine *Engine) editChapter(ctx context.Context, bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork) (chapterWork, error) {
	budget := newInterventionBudget()
	draftNumber := 1

	for {
		// 每个候选稿先通过纯技术校验；失败不会消耗文学干预预算。
		if err := validateDraft(work.chapter); err != nil {
			return work, err
		}

		// 两次干预已经用尽时，最终稿自动通过，只让 Editor 提炼长期状态。
		if budget.Remaining() == 0 {
			return engine.finalizeAutoAccepted(ctx, bible, current, reader, work)
		}

		decision, err := engine.reviewDraft(ctx, bible, current, reader, work)
		if err != nil {
			return work, err
		}
		work.reviewLog.Decisions = append(work.reviewLog.Decisions, decisionRecord(decision))
		if err := engine.saveEditorDecision(work.context.NextChapter, len(work.reviewLog.Decisions), decision); err != nil {
			return work, err
		}

		if decision.Action == story.EditorAccept {
			work.update, work.summary = decision.StoryUpdate, decision.Summary
			work.liveTensions = append([]string(nil), decision.LiveTensions...)
			return work, nil
		}

		if err := budget.Consume(decision.Action); err != nil {
			return work, err
		}
		work.reviewLog.Interventions++
		draftNumber++

		work, err = engine.applyIntervention(ctx, bible, current, reader, work, decision)
		if err != nil {
			return work, err
		}
		if err := engine.saveDraft(work.context.NextChapter, draftNumber, work.chapter); err != nil {
			return work, err
		}
	}
}

func (engine *Engine) reviewDraft(ctx context.Context, bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork) (story.EditorDecision, error) {
	input, err := asPrettyJSON(newEditorInput(bible, current, reader, work))
	if err != nil {
		return story.EditorDecision{}, err
	}

	reviewNumber := len(work.reviewLog.Decisions) + 1
	stage := chapterStage(work.context.NextChapter, fmt.Sprintf("editor_review_%d", reviewNumber))
	decision, err := generateJSON[story.EditorDecision](ctx, engine, stage, "editor", "editor_decision", input, editorMaxOutputTokens, "low")
	if err != nil {
		return story.EditorDecision{}, err
	}
	if err := story.ValidateEditorDecision(decision, work.context.NextChapter); err != nil {
		return story.EditorDecision{}, fmt.Errorf("Editor Decision 无效: %w", err)
	}

	return decision, nil
}

func newEditorInput(bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork) editorInput {
	return editorInput{
		Bible: bible, Outline: work.outline, State: current,
		RecentSummaries: work.context.RecentSummaries, PreviousChapter: work.context.PreviousChapter,
		PreviousReaderObservation: reader, CurrentDraft: work.chapter,
	}
}

func decisionRecord(decision story.EditorDecision) story.EditorDecisionRecord {
	return story.EditorDecisionRecord{
		Action: decision.Action, Reason: decision.Reason, Guidance: decision.Guidance,
	}
}

func (engine *Engine) applyIntervention(ctx context.Context, bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork, decision story.EditorDecision) (chapterWork, error) {
	if decision.Action == story.EditorRevise {
		chapter, err := engine.reviseDraft(ctx, work, decision.Guidance)
		work.chapter = chapter
		return work, err
	}

	outline, err := engine.replanOutline(ctx, bible, current, reader, work, decision.Guidance)
	if err != nil {
		return work, err
	}
	work.outline, work.replanned = outline, true
	work.context.Outline = outline

	chapter, err := engine.rewriteAfterReplan(ctx, work, decision.Guidance)
	work.chapter = chapter
	return work, err
}

func (engine *Engine) finalizeAutoAccepted(ctx context.Context, bible story.StoryBible, current story.State, reader *story.ReaderObservation, work chapterWork) (chapterWork, error) {
	input, err := asPrettyJSON(newEditorInput(bible, current, reader, work))
	if err != nil {
		return work, err
	}

	result, err := generateJSON[story.EditorFinalizeResult](ctx, engine, chapterStage(work.context.NextChapter, "editor_finalize"), "editor_finalize", "editor_finalize", input, editorMaxOutputTokens, "low")
	if err != nil {
		return work, err
	}
	if err := story.ValidateEditorFinalize(result, work.context.NextChapter); err != nil {
		return work, fmt.Errorf("Editor Finalize 无效: %w", err)
	}

	work.update, work.summary = result.StoryUpdate, result.Summary
	work.liveTensions = append([]string(nil), result.LiveTensions...)
	work.reviewLog.AutoAccepted = true
	return work, nil
}

func (engine *Engine) saveEditorDecision(number, reviewNumber int, decision story.EditorDecision) error {
	name := fmt.Sprintf("editor-review-%d.json", reviewNumber)
	return engine.store.SaveWorking(number, name, decision)
}
