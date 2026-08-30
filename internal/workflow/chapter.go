package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/story"
)

const (
	// 摘要窗口只控制重复上下文大小，不限制故事能回忆的事实；完整正典仍在 State 中。
	recentSummaryLimit = 3

	replanMaxOutputTokens = 8000
	// Recorder 输出的是机器状态差量。该上限只防止供应商无限输出，
	// 不限制正文章节长度、事件数量或人物数量。
	recorderMaxOutputTokens = 12000
	canonMaxOutputTokens    = 5000
	readerMaxOutputTokens   = 3000
	chapterMaxOutputTokens  = 12000

	writerTemperature   = 0.95
	revisionTemperature = 0.75
)

type recorderInput struct {
	Context writerContext `json:"context"`
	Chapter string        `json:"chapter_text"`
}

type recorderRepairInput struct {
	Source          recorderInput    `json:"source"`
	RejectedDelta   story.StateDelta `json:"rejected_delta"`
	ValidationError string           `json:"validation_error"`
}

type canonInput struct {
	Context   writerContext    `json:"context"`
	Chapter   string           `json:"chapter_text"`
	Delta     story.StateDelta `json:"proposed_state_delta"`
	NextState story.State      `json:"candidate_next_state"`
}

type revisionInput struct {
	Context writerContext     `json:"context"`
	Chapter string            `json:"current_chapter"`
	Review  story.CanonReview `json:"canon_review"`
}

// readerInput 是严格的读者可见投影。它不含作者承诺、历史主观评价、
// active outline、隐藏事实或完整正典状态，防止 Reader 被既有结论锚定。
type readerInput struct {
	TargetReader           story.TargetReader     `json:"target_reader"`
	PreviousChapter        string                 `json:"previous_chapter"`
	RecentSummaries        []story.ChapterSummary `json:"reader_visible_recent_summaries"`
	EarlyRelevantSummaries []story.ChapterSummary `json:"reader_visible_early_retrieval"`
	CurrentNumber          int                    `json:"current_chapter_number"`
	CurrentChapter         string                 `json:"current_chapter"`
}

type replanInput struct {
	Bible                story.StoryBible       `json:"story_bible"`
	OldOutline           story.StoryOutline     `json:"old_outline"`
	CompletedMovementIDs []string               `json:"completed_movement_ids"`
	CanonState           story.State            `json:"canonical_state"`
	RecentSummaries      []story.ChapterSummary `json:"recent_summaries"`
}

// chapterWork 汇集尚未提交的本章事务产物。字段只在 .work 阶段流转，
// CommitChapter 成功后才会成为 HEAD 可见历史。
type chapterWork struct {
	context writerContext
	// canonicalBase 保留本章开始前的完整状态。Writer 可以只看摘要窗口，
	// 但 Recorder 应用差量时不能把这个上下文投影误当成正式历史。
	canonicalBase     story.State
	outline           story.StoryOutline
	replanned         bool
	chapter           string
	delta             story.StateDelta
	review            story.CanonReview
	readerObservation story.ReaderObservation
	next              story.State
}

// Run advances by at most limit chapters. A zero limit follows the story's own
// completion state rather than an artificial chapter target.
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

// loadRunState 读取继续运行所需的 project、Bible 和 HEAD 状态。
func (engine *Engine) loadRunState() (story.Project, story.StoryBible, story.State, error) {
	// 分层读取配置、Bible 和 HEAD 检查点，任何一层失败都立即停止，
	// 避免用零值继续生成出与项目无关的正文。
	// 读取不可变项目配置。
	project, err := engine.store.LoadProject()
	if err != nil {
		return story.Project{}, story.StoryBible{}, story.State{}, err
	}

	// 读取全书 Bible。
	bible, err := engine.store.LoadBible()
	if err != nil {
		return story.Project{}, story.StoryBible{}, story.State{}, err
	}

	// 根据 HEAD 读取当前状态快照。
	state, err := engine.store.LoadState()
	return project, bible, state, err
}

func (engine *Engine) runChapter(ctx context.Context, project story.Project, bible story.StoryBible, current story.State, reader *story.ReaderObservation) (story.State, *story.ReaderObservation, error) {
	number := current.Chapter + 1
	engine.emit("chapter", fmt.Sprintf("开始第 %d 章", number))

	// 先完成创作、状态提取和正典检查。任何中间产物都只进入 .work，
	// 因而后续 Reader 或提交失败时，HEAD 仍停留在上一完整章节。
	work, err := engine.prepareChapter(ctx, project, bible, current, reader)
	if err != nil {
		return story.State{}, nil, err
	}

	// 正典失败只允许整章修订一次，避免多个角色反复改稿吞噬创作空间。
	if !work.review.Passed {
		work, err = engine.reviseChapter(ctx, work)
		if err != nil {
			return story.State{}, nil, err
		}
	}
	if !work.review.Passed {
		return story.State{}, nil, fmt.Errorf("第 %d 章修订后仍违反正典，现场已保存在 .work", number)
	}

	// Reader 在正文定稿后观察体验；它不否决本章，状态只供下一章参考。
	work.readerObservation, err = engine.observeReader(ctx, bible.TargetReader, work.context.PreviousChapter, current.Summaries, work.chapter, number)
	if err != nil {
		return story.State{}, nil, err
	}
	if err := engine.store.CommitChapter(work.chapter, work.delta, work.review, work.readerObservation, work.next, work.outline, work.replanned); err != nil {
		return story.State{}, nil, err
	}

	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交；读者观察已记录", number))
	return work.next, &work.readerObservation, nil
}

func (engine *Engine) prepareChapter(ctx context.Context, project story.Project, bible story.StoryBible, current story.State, reader *story.ReaderObservation) (chapterWork, error) {
	// 阶段一：只有 movement 已完成或阻塞时，才为未来路线生成一个候选
	// 新版本；Reader 只观察体验，不能触发 Replanner 或控制工作流。
	outline, err := engine.store.LoadOutline(current.OutlineVersion)
	if err != nil {
		return chapterWork{}, err
	}
	outline, base, replanned, err := engine.maybeReplan(ctx, bible, outline, current)
	if err != nil {
		return chapterWork{}, err
	}

	// 阶段二：Writer 直接使用持久大纲、正典状态和上一章 Reader Observation 自由写作。
	chapterCtx, err := engine.buildWriterContext(project, bible, outline, base, reader)
	if err != nil {
		return chapterWork{}, err
	}
	chapter, err := engine.writeChapter(ctx, chapterCtx)
	if err != nil {
		return chapterWork{}, err
	}
	work := chapterWork{context: chapterCtx, canonicalBase: base, outline: outline, replanned: replanned, chapter: chapter}
	if err := engine.store.SaveWorking(chapterCtx.NextChapter, "draft.md", chapter); err != nil {
		return chapterWork{}, err
	}

	// 阶段三：正文先被 Recorder 记账，再由 Canon Checker 检查。
	return engine.inspectChapter(ctx, work, "initial")
}

func (engine *Engine) maybeReplan(ctx context.Context, bible story.StoryBible, outline story.StoryOutline, current story.State) (story.StoryOutline, story.State, bool, error) {
	triggered := current.OutlineProgress.Status == "completed" || current.OutlineProgress.Status == "blocked"
	if !triggered {
		return outline, current, false, nil
	}
	input, err := asPrettyJSON(replanInput{Bible: bible, OldOutline: outline,
		CompletedMovementIDs: current.CompletedMovementIDs, CanonState: current,
		RecentSummaries: recentSummaries(current.Summaries, recentSummaryLimit)})
	if err != nil {
		return story.StoryOutline{}, story.State{}, false, err
	}
	next, err := generateJSON[story.StoryOutline](ctx, engine, chapterStage(current.Chapter+1, "replan"), "replanner", "story_outline", input, replanMaxOutputTokens, "low")
	if err != nil {
		return story.StoryOutline{}, story.State{}, false, err
	}
	next.Version = current.Chapter + 1
	if err := story.ValidateReplan(outline, next, current.CompletedMovementIDs); err != nil {
		return story.StoryOutline{}, story.State{}, false, err
	}

	// 新大纲只是本章事务的候选输入。CommitChapter 成功前不单独持久化，
	// 因而 Reader/Canon 失败不会让大纲版本领先于 HEAD。
	base := current
	base.OutlineVersion = next.Version
	base.OutlineProgress = story.OutlineProgress{CurrentMovementID: next.CurrentMovementID, Status: "ongoing"}
	return next, base, true, nil
}

func (engine *Engine) writeChapter(ctx context.Context, chapterCtx writerContext) (string, error) {
	input, err := asPrettyJSON(chapterCtx)
	if err != nil {
		return "", err
	}
	return generateText(ctx, engine, chapterStage(chapterCtx.NextChapter, "write"), "writer", input, chapterMaxOutputTokens, writerTemperature)
}

func (engine *Engine) inspectChapter(ctx context.Context, work chapterWork, attempt string) (chapterWork, error) {
	// Recorder 负责把文学文本投影为结构状态；Go 只验证引用和状态迁移，
	// 不假装能用字符串规则判断某个情节是否真的发生。
	delta, next, err := engine.extractValidDelta(ctx, work)
	if err != nil {
		return work, err
	}
	review, err := engine.checkCanon(ctx, work, delta, next)
	if err != nil {
		return work, err
	}
	review = mergeDeterministicIssues(review, append(deterministicIssues(work.chapter), terminalClosureIssues(next)...))
	work.delta, work.next, work.review = delta, next, review
	if err := engine.store.SaveWorking(work.context.NextChapter, "delta-"+attempt+".json", delta); err != nil {
		return work, err
	}
	if err := engine.store.SaveWorking(work.context.NextChapter, "canon-"+attempt+".json", review); err != nil {
		return work, err
	}
	return work, nil
}

func (engine *Engine) extractValidDelta(ctx context.Context, work chapterWork) (story.StateDelta, story.State, error) {
	delta, err := engine.extractDelta(ctx, work)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	delta, err = engine.normalizeDelta(work.context.NextChapter, work.canonicalBase, delta)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	next, validationErr := story.ApplyDelta(work.canonicalBase, delta)
	if validationErr == nil {
		return delta, next, nil
	}

	repaired, err := engine.repairDelta(ctx, work, delta, validationErr)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	repaired, err = engine.normalizeDelta(work.context.NextChapter, work.canonicalBase, repaired)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	next, err = story.ApplyDelta(work.canonicalBase, repaired)
	if err != nil {
		return story.StateDelta{}, story.State{}, fmt.Errorf("书记员状态修复后仍无效: %w", err)
	}

	return repaired, next, nil
}

func (engine *Engine) extractDelta(ctx context.Context, work chapterWork) (story.StateDelta, error) {
	input, err := asPrettyJSON(recorderInput{Context: work.context, Chapter: work.chapter})
	if err != nil {
		return story.StateDelta{}, err
	}
	return generateJSON[story.StateDelta](ctx, engine, chapterStage(work.context.NextChapter, "record"), "recorder", "state_delta", input, recorderMaxOutputTokens, "none")
}

func (engine *Engine) repairDelta(ctx context.Context, work chapterWork, rejected story.StateDelta, validationErr error) (story.StateDelta, error) {
	input, err := asPrettyJSON(recorderRepairInput{Source: recorderInput{Context: work.context, Chapter: work.chapter}, RejectedDelta: rejected, ValidationError: validationErr.Error()})
	if err != nil {
		return story.StateDelta{}, err
	}
	return generateJSON[story.StateDelta](ctx, engine, chapterStage(work.context.NextChapter, "record_repair"), "recorder", "state_delta", input, recorderMaxOutputTokens, "none")
}

func (engine *Engine) normalizeDelta(number int, current story.State, delta story.StateDelta) (story.StateDelta, error) {
	normalized, characters := story.DropUnknownCharacterStates(current, delta)
	normalized, relationships := story.DropUnknownRelationships(current, normalized)
	normalized, threads := story.DropDuplicatedNewThreadUpdates(current, normalized)
	if len(characters)+len(relationships)+len(threads) == 0 {
		return normalized, nil
	}
	diagnostics := map[string][]string{"unknown_characters": characters, "unknown_relationships": relationships, "duplicate_threads": threads}
	return normalized, engine.store.SaveWorking(number, "delta-normalization.json", diagnostics)
}

func (engine *Engine) checkCanon(ctx context.Context, work chapterWork, delta story.StateDelta, next story.State) (story.CanonReview, error) {
	input, err := asPrettyJSON(canonInput{Context: work.context, Chapter: work.chapter, Delta: delta, NextState: next})
	if err != nil {
		return story.CanonReview{}, err
	}
	return generateJSON[story.CanonReview](ctx, engine, chapterStage(work.context.NextChapter, "canon"), "canon_checker", "canon_review", input, canonMaxOutputTokens, "none")
}

func (engine *Engine) reviseChapter(ctx context.Context, work chapterWork) (chapterWork, error) {
	input, err := asPrettyJSON(revisionInput{Context: work.context, Chapter: work.chapter, Review: work.review})
	if err != nil {
		return work, err
	}
	work.chapter, err = generateText(ctx, engine, chapterStage(work.context.NextChapter, "revise"), "revision", input, chapterMaxOutputTokens, revisionTemperature)
	if err != nil {
		return work, err
	}
	return engine.inspectChapter(ctx, work, "revised")
}

func (engine *Engine) observeReader(ctx context.Context, targetReader story.TargetReader, previousChapter string, summaries []story.ChapterSummary, chapter string, number int) (story.ReaderObservation, error) {
	recent, early := readerVisibleHistory(chapter, summaries)
	input, err := asPrettyJSON(readerInput{
		TargetReader: targetReader, PreviousChapter: previousChapter,
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

	// Reader 只交付主观观察。程序验证章节归属，不从任何感受字段推导
	// Writer 指令或 Replanner 动作。
	if err := story.ValidateReaderObservation(observation, number); err != nil {
		return story.ReaderObservation{}, err
	}

	return observation, nil
}

func chapterStage(number int, stage string) string {
	return fmt.Sprintf("chapter_%03d_%s", number, stage)
}
