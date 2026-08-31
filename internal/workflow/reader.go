package workflow

import (
	"context"

	"story_emerge/internal/story"
)

const readerMaxOutputTokens = 3000

// readerInput 是严格的读者可见投影，不包含任何作者侧信息或历史主观评价。
type readerInput struct {
	TargetReader           story.TargetReader     `json:"target_reader"`
	PreviousChapter        string                 `json:"previous_chapter"`
	RecentSummaries        []story.ChapterSummary `json:"reader_visible_recent_summaries"`
	EarlyRelevantSummaries []story.ChapterSummary `json:"reader_visible_early_retrieval"`
	CurrentNumber          int                    `json:"current_chapter_number"`
	CurrentChapter         string                 `json:"current_chapter"`
}

func (engine *Engine) observeReader(ctx context.Context, target story.TargetReader, previousChapter, chapter string, number int) (story.ReaderObservation, error) {
	// Reader 的历史只来自已提交正文摘要，不读取上一轮 Reader Observation。
	summaries, err := engine.store.LoadSummaries()
	if err != nil {
		return story.ReaderObservation{}, err
	}
	recent, early := visibleHistory(chapter, summaries, number-1)

	input, err := asPrettyJSON(readerInput{
		TargetReader: target, PreviousChapter: previousChapter,
		RecentSummaries: recent, EarlyRelevantSummaries: early,
		CurrentNumber: number, CurrentChapter: chapter,
	})
	if err != nil {
		return story.ReaderObservation{}, err
	}

	observation, err := generateJSON[story.ReaderObservation](ctx, engine, chapterStage(number, "reader"), "reader", "reader_observation", input, readerMaxOutputTokens, "none")
	if err != nil {
		return story.ReaderObservation{}, err
	}
	if err := story.ValidateReaderObservation(observation, number); err != nil {
		return story.ReaderObservation{}, err
	}

	return observation, nil
}
