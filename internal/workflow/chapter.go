package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/story"
)

// chapterWork 汇集一个尚未提交的章节事务。
type chapterWork struct {
	context     writerContext
	outline     story.StoryOutline
	replanned   bool
	chapter     string
	update      story.StoryUpdate
	summary     story.ChapterSummary
	reviewLog   story.EditorReviewLog
	next        story.State
	observation story.ReaderObservation
}

// Run 最多推进 limit 章；limit=0 时由故事自身的完成状态决定终点。
func (engine *Engine) Run(ctx context.Context, limit int) error {
	project, bible, state, err := engine.loadRunState()
	if err != nil {
		return err
	}

	reader, err := engine.store.LoadLatestReaderObservation()
	if err != nil {
		return err
	}

	for generated := 0; state.StoryStatus != "completed" && (limit == 0 || generated < limit); generated++ {
		state, reader, err = engine.runChapter(ctx, project, bible, state, reader)
		if err != nil {
			return err
		}
	}

	engine.emit("complete", fmt.Sprintf("已写到第 %d 章，故事状态 %s", state.Chapter, state.StoryStatus))
	return nil
}

func (engine *Engine) loadRunState() (story.Project, story.StoryBible, story.State, error) {
	project, err := engine.store.LoadProject()
	if err != nil {
		return story.Project{}, story.StoryBible{}, story.State{}, err
	}

	bible, err := engine.store.LoadBible()
	if err != nil {
		return story.Project{}, story.StoryBible{}, story.State{}, err
	}

	state, err := engine.store.LoadState()
	return project, bible, state, err
}

func (engine *Engine) runChapter(ctx context.Context, project story.Project, bible story.StoryBible, current story.State, reader *story.ReaderObservation) (story.State, *story.ReaderObservation, error) {
	number := current.Chapter + 1
	engine.emit("chapter", fmt.Sprintf("开始第 %d 章", number))

	// 组装 Writer 所需的故事方向、长期状态和近期上下文。
	work, err := engine.startChapter(ctx, project, bible, current, reader)
	if err != nil {
		return story.State{}, nil, err
	}

	// 在最多两次文学干预内，让 Editor 决定接受、修订或按需重规划。
	work, err = engine.editChapter(ctx, bible, current, reader, work)
	if err != nil {
		return story.State{}, nil, err
	}

	// 最终正文确定后，只应用该版本产生的长期 Story Update。
	work.next, err = story.ApplyStoryUpdate(current, work.outline, work.update)
	if err != nil {
		return story.State{}, nil, fmt.Errorf("Editor Story Update 无效: %w", err)
	}

	// Reader 独立阅读最终正文；它不读取 Editor、Bible、Outline 或 Story State。
	work.observation, err = engine.observeReader(ctx, bible.TargetReader, work.context.PreviousChapter, work.chapter, number)
	if err != nil {
		return story.State{}, nil, err
	}

	// 全部正式产物准备完成后才推进 HEAD，任何失败都保留上一章完整状态。
	err = engine.store.CommitChapter(work.chapter, work.update, work.summary, work.reviewLog, work.observation, work.next, work.outline, work.replanned)
	if err != nil {
		return story.State{}, nil, err
	}

	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交；Editor 干预 %d 次", number, work.reviewLog.Interventions))
	return work.next, &work.observation, nil
}

func (engine *Engine) startChapter(ctx context.Context, project story.Project, bible story.StoryBible, current story.State, reader *story.ReaderObservation) (chapterWork, error) {
	outline, err := engine.store.LoadOutline(current.OutlineVersion)
	if err != nil {
		return chapterWork{}, err
	}

	chapterContext, err := engine.buildWriterContext(project, bible, outline, current, reader)
	if err != nil {
		return chapterWork{}, err
	}

	draft, err := engine.writeDraft(ctx, chapterContext)
	if err != nil {
		return chapterWork{}, err
	}
	if err := engine.saveDraft(chapterContext.NextChapter, 1, draft); err != nil {
		return chapterWork{}, err
	}

	return chapterWork{
		context: chapterContext, outline: outline, chapter: draft,
		reviewLog: story.EditorReviewLog{Chapter: chapterContext.NextChapter},
	}, nil
}

func chapterStage(number int, stage string) string {
	return fmt.Sprintf("chapter_%03d_%s", number, stage)
}
