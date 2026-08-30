package workflow

import (
	"context"
	"fmt"
	"strings"

	"story_emerge/internal/story"
)

type chapterDraftInput struct {
	Context       chapterContext    `json:"context"`
	Plan          story.ChapterPlan `json:"approved_plan"`
	LengthControl string            `json:"length_control"`
}

type recorderInput struct {
	Context chapterContext    `json:"context"`
	Plan    story.ChapterPlan `json:"approved_plan"`
	Chapter string            `json:"chapter_text"`
}

type recorderRepairInput struct {
	Context         chapterContext    `json:"context"`
	Plan            story.ChapterPlan `json:"approved_plan"`
	Chapter         string            `json:"chapter_text"`
	RejectedDelta   story.StateDelta  `json:"rejected_delta"`
	ValidationError string            `json:"validation_error"`
	RepairRule      string            `json:"repair_rule"`
}

type editorInput struct {
	Context          chapterContext    `json:"context"`
	Plan             story.ChapterPlan `json:"approved_plan"`
	Chapter          string            `json:"chapter_text"`
	Delta            story.StateDelta  `json:"proposed_state_delta"`
	VisibleChars     int               `json:"exact_visible_chars"`
	AllowedCharRange string            `json:"allowed_char_range"`
}

type revisionInput struct {
	Context       chapterContext    `json:"context"`
	Plan          story.ChapterPlan `json:"approved_plan"`
	Chapter       string            `json:"current_chapter"`
	Review        story.Review      `json:"editor_review"`
	LengthControl string            `json:"length_control"`
}

// Run 从 HEAD 继续写作；limit=0 表示一直写到项目目标章节。
func (engine *Engine) Run(ctx context.Context, limit int) error {
	project, bible, state, err := engine.loadRunState()
	if err != nil {
		return err
	}

	remaining := project.TargetChapters - state.Chapter
	if limit > 0 && limit < remaining {
		remaining = limit
	}

	for index := 0; index < remaining; index++ {
		next, err := engine.runChapter(ctx, project, bible, state)
		if err != nil {
			return err
		}
		state = next
	}

	engine.emit("complete", fmt.Sprintf("已写到第 %d 章", state.Chapter))
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

func (engine *Engine) runChapter(
	ctx context.Context,
	project story.Project,
	bible story.StoryBible,
	current story.State,
) (story.State, error) {
	number := current.Chapter + 1
	engine.emit("chapter", fmt.Sprintf("开始第 %d 章", number))
	working, err := engine.prepareChapter(ctx, project, bible, current)
	if err != nil {
		return story.State{}, err
	}

	if !working.review.Passed {
		working, err = engine.reviseChapter(ctx, project, working)
		if err != nil {
			return story.State{}, err
		}
	}

	if canMinorCorrect(project, working) {
		working, err = engine.correctChapter(ctx, project, working)
		if err != nil {
			return story.State{}, err
		}
	}
	if !working.review.Passed {
		return story.State{}, fmt.Errorf("第 %d 章经过有界修订后仍未通过，现场已保存在 .work", number)
	}

	if err := engine.reflectArc(ctx, &working); err != nil {
		return story.State{}, err
	}

	if err := engine.store.CommitChapter(working.plan, working.chapter, working.delta, working.review, working.next); err != nil {
		return story.State{}, err
	}

	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交，评分 %d", number, working.review.Score))
	return working.next, nil
}

type chapterWork struct {
	context chapterContext
	plan    story.ChapterPlan
	chapter string
	delta   story.StateDelta
	review  story.Review
	next    story.State
}

func (engine *Engine) prepareChapter(
	ctx context.Context,
	project story.Project,
	bible story.StoryBible,
	current story.State,
) (chapterWork, error) {
	chapterCtx, err := engine.buildChapterContext(project, bible, current)
	if err != nil {
		return chapterWork{}, err
	}
	plan, err := engine.planChapter(ctx, project, chapterCtx)
	if err != nil {
		return chapterWork{}, err
	}
	chapter, err := engine.writeChapter(ctx, project, chapterCtx, plan)
	if err != nil {
		return chapterWork{}, err
	}
	work := chapterWork{context: chapterCtx, plan: plan, chapter: chapter}
	if err := engine.store.SaveWorking(plan.Number, "draft.md", chapter); err != nil {
		return chapterWork{}, err
	}
	return engine.inspectChapter(ctx, project, work, "initial")
}

func (engine *Engine) planChapter(
	ctx context.Context,
	project story.Project,
	chapterCtx chapterContext,
) (story.ChapterPlan, error) {
	input, err := asPrettyJSON(chapterCtx)
	if err != nil {
		return story.ChapterPlan{}, err
	}
	stage := chapterStage(chapterCtx.NextChapter, "plan")
	// 章节策划的输出结构已经足够约束，不需要额外长链推理。
	// 对 Flash 模型使用 none，避免推理 token 挤占最终 JSON 的输出空间。
	plan, err := generateJSON[story.ChapterPlan](ctx, engine, stage, "planner", "chapter_plan", input, 7000, "none")
	if err != nil {
		return story.ChapterPlan{}, err
	}
	if err := validatePlan(project, chapterCtx.State, plan); err != nil {
		return story.ChapterPlan{}, fmt.Errorf("第 %d 章计划无效: %w", chapterCtx.NextChapter, err)
	}
	if err := engine.store.SaveWorking(plan.Number, "plan.json", plan); err != nil {
		return story.ChapterPlan{}, err
	}
	return plan, nil
}

func (engine *Engine) writeChapter(
	ctx context.Context,
	project story.Project,
	chapterCtx chapterContext,
	plan story.ChapterPlan,
) (string, error) {
	input, err := asPrettyJSON(chapterDraftInput{
		Context: chapterCtx, Plan: plan, LengthControl: lengthControl(project),
	})
	if err != nil {
		return "", err
	}
	maxTokens := chapterTextTokenLimit(project)
	return generateText(ctx, engine, chapterStage(plan.Number, "write"), "writer", input, maxTokens, 0.9)
}

func (engine *Engine) inspectChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
	attempt string,
) (chapterWork, error) {
	delta, next, err := engine.extractValidDelta(ctx, work)
	if err != nil {
		return work, err
	}
	review, err := engine.reviewChapter(ctx, project, work, delta)
	if err != nil {
		return work, err
	}
	review = mergeDeterministicIssues(review, terminalClosureIssues(project, next))
	work.delta, work.next, work.review = delta, next, review
	if err := engine.saveInspection(work.plan.Number, attempt, delta, review); err != nil {
		return work, err
	}
	return work, nil
}

// extractValidDelta 最多修复一次机器状态；正文和章节计划在此阶段保持不变。
func (engine *Engine) extractValidDelta(
	ctx context.Context,
	work chapterWork,
) (story.StateDelta, story.State, error) {
	delta, err := engine.extractDelta(ctx, work)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	delta, err = engine.normalizeDelta(work.plan.Number, work.context.State, delta)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	next, validationErr := story.ApplyDelta(work.context.State, delta)
	if validationErr == nil {
		return delta, next, nil
	}
	if err := engine.store.SaveWorking(work.plan.Number, "delta-invalid.json", delta); err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	repaired, err := engine.repairDelta(ctx, work, delta, validationErr)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	repaired, err = engine.normalizeDelta(work.plan.Number, work.context.State, repaired)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	next, err = story.ApplyDelta(work.context.State, repaired)
	if err != nil {
		_ = engine.store.SaveWorking(work.plan.Number, "delta-repair-invalid.json", repaired)
		return story.StateDelta{}, story.State{}, fmt.Errorf("书记员状态修复后仍无效: %w", err)
	}
	return repaired, next, nil
}

func (engine *Engine) normalizeDelta(
	number int,
	current story.State,
	delta story.StateDelta,
) (story.StateDelta, error) {
	normalized, characters := story.DropUnknownCharacterStates(current, delta)
	normalized, relationships := story.DropUnknownRelationships(current, normalized)
	normalized, threads := story.DropDuplicatedNewThreadUpdates(current, normalized)
	if len(characters)+len(relationships)+len(threads) == 0 {
		return normalized, nil
	}
	diagnostics := struct {
		Characters    []string `json:"dropped_unknown_character_states"`
		Relationships []string `json:"dropped_unknown_relationships"`
		Threads       []string `json:"dropped_duplicate_new_thread_updates"`
	}{Characters: characters, Relationships: relationships, Threads: threads}
	count := len(characters) + len(relationships) + len(threads)
	engine.emit(chapterStage(number, "record"), fmt.Sprintf("归一化 %d 条状态字段", count))
	if err := engine.store.SaveWorking(number, "delta-normalization.json", diagnostics); err != nil {
		return story.StateDelta{}, err
	}
	return normalized, nil
}

func (engine *Engine) saveInspection(
	number int,
	attempt string,
	delta story.StateDelta,
	review story.Review,
) error {
	if err := engine.store.SaveWorking(number, "delta-"+attempt+".json", delta); err != nil {
		return err
	}
	return engine.store.SaveWorking(number, "review-"+attempt+".json", review)
}

func (engine *Engine) extractDelta(ctx context.Context, work chapterWork) (story.StateDelta, error) {
	input, err := asPrettyJSON(recorderInput{Context: work.context, Plan: work.plan, Chapter: work.chapter})
	if err != nil {
		return story.StateDelta{}, err
	}
	stage := chapterStage(work.plan.Number, "record")
	return generateJSON[story.StateDelta](ctx, engine, stage, "recorder", "state_delta", input, 6500, "none")
}

func (engine *Engine) repairDelta(
	ctx context.Context,
	work chapterWork,
	rejected story.StateDelta,
	validationErr error,
) (story.StateDelta, error) {
	input, err := asPrettyJSON(recorderRepairInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter,
		RejectedDelta: rejected, ValidationError: validationErr.Error(),
		RepairRule: "只修复状态 JSON；knowledge 与 relationships 只写本章增量，旧条目由程序自动保留；不得改变正文已经发生的事实",
	})
	if err != nil {
		return story.StateDelta{}, err
	}
	stage := chapterStage(work.plan.Number, "record_repair")
	return generateJSON[story.StateDelta](ctx, engine, stage, "recorder", "state_delta", input, 6500, "none")
}

func (engine *Engine) reviewChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
	delta story.StateDelta,
) (story.Review, error) {
	input, err := asPrettyJSON(editorInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Delta: delta,
		VisibleChars:     visibleRuneCount(work.chapter),
		AllowedCharRange: fmt.Sprintf("%d-%d", acceptedMinChars(project), acceptedMaxChars(project)),
	})
	if err != nil {
		return story.Review{}, err
	}
	stage := chapterStage(work.plan.Number, "review")
	review, err := generateJSON[story.Review](ctx, engine, stage, "editor", "review", input, 6500, "low")
	if err != nil {
		return story.Review{}, err
	}
	issues := deterministicIssues(work.chapter, acceptedMinChars(project), acceptedMaxChars(project))
	return mergeDeterministicIssues(review, issues), nil
}

func (engine *Engine) reviseChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
) (chapterWork, error) {
	input, err := asPrettyJSON(revisionInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Review: work.review,
		LengthControl: lengthControl(project),
	})
	if err != nil {
		return work, err
	}
	maxTokens := chapterTextTokenLimit(project)
	stage := chapterStage(work.plan.Number, "revise")
	chapter, err := generateText(ctx, engine, stage, "revision", input, maxTokens, 0.8)
	if err != nil {
		return work, err
	}
	work.chapter = chapter
	if err := engine.store.SaveWorking(work.plan.Number, "revised.md", chapter); err != nil {
		return work, err
	}
	return engine.inspectChapter(ctx, project, work, "revised")
}

// correctChapter 只处理二审后剩下的单个局部硬伤，不承担结构性重写。
func (engine *Engine) correctChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
) (chapterWork, error) {
	input, err := asPrettyJSON(revisionInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Review: work.review,
		LengthControl: lengthControl(project),
	})
	if err != nil {
		return work, err
	}

	stage := chapterStage(work.plan.Number, "correct")
	chapter, err := generateText(ctx, engine, stage, "revision", input, chapterTextTokenLimit(project), 0.4)
	if err != nil {
		return work, err
	}

	work.chapter = chapter
	if err := engine.store.SaveWorking(work.plan.Number, "corrected.md", chapter); err != nil {
		return work, err
	}

	return engine.inspectChapter(ctx, project, work, "corrected")
}

func canMinorCorrect(project story.Project, work chapterWork) bool {
	count := visibleRuneCount(work.chapter)
	return !work.review.Passed && work.review.Score >= 70 && len(work.review.HardIssues) == 1 &&
		count >= acceptedMinChars(project) && count <= acceptedMaxChars(project)
}

func (engine *Engine) reflectArc(ctx context.Context, work *chapterWork) error {
	if work.next.Chapter%4 != 0 && work.next.Chapter != work.context.TargetChapters {
		return nil
	}
	input, err := asPrettyJSON(struct {
		Context chapterContext `json:"context"`
		State   story.State    `json:"candidate_state"`
	}{Context: work.context, State: work.next})
	if err != nil {
		return err
	}
	stage := chapterStage(work.next.Chapter, "arc_review")
	reflection, err := generateJSON[story.ArcReflection](ctx, engine, stage, "arc_review", "arc_review", input, 5500, "low")
	if err != nil {
		return err
	}
	work.next.DirectorGuidance = append([]string(nil), reflection.Priorities...)
	for _, warning := range reflection.Warnings {
		work.next.DirectorGuidance = append(work.next.DirectorGuidance, "避免："+warning)
	}
	return engine.store.SaveWorking(work.next.Chapter, "arc-review.json", reflection)
}

func validatePlan(project story.Project, state story.State, plan story.ChapterPlan) error {
	if plan.Number != state.Chapter+1 {
		return fmt.Errorf("章节号应为 %d", state.Chapter+1)
	}
	if plan.TargetChars < project.ChapterMinChars || plan.TargetChars > project.ChapterMaxChars {
		return fmt.Errorf("目标字数 %d 不在项目范围内", plan.TargetChars)
	}
	known := make(map[string]bool, len(state.Threads))
	for _, thread := range state.Threads {
		known[thread.ID] = true
	}
	for _, threadID := range plan.ActiveThreads {
		if !known[threadID] {
			return fmt.Errorf("引用未知剧情线: %s", threadID)
		}
	}
	if len(plan.Scenes) < 3 || strings.TrimSpace(plan.Hook) == "" {
		return fmt.Errorf("至少需要三个场景和明确尾钩")
	}
	return nil
}

func chapterStage(number int, action string) string {
	return fmt.Sprintf("chapter_%03d_%s", number, action)
}

func lengthControl(project story.Project) string {
	minTokens := project.ChapterMinChars * 13 / 20
	maxTokens := project.ChapterMaxChars * 13 / 20
	return fmt.Sprintf(
		"正文必须为 %d～%d 个非空白可见字符；为避免把字符误当 token，请将正文控制在约 %d～%d 个中文输出 token",
		project.ChapterMinChars, project.ChapterMaxChars, minTokens, maxTokens,
	)
}

// chapterTextTokenLimit 是字符上限之外的第二道保险。
// 实测中文类型小说约消耗 0.65～0.75 token/可见字符，预留少量波动后仍能阻止失控扩写。
func chapterTextTokenLimit(project story.Project) int {
	return max(1800, project.ChapterMaxChars*6/7)
}

// acceptedMaxChars 给生成文本保留 10% 的自然波动，但写作 Prompt 仍以项目上限为目标。
// 这避免为了几十个字符反复整章重写，同时又不会接受明显失控的篇幅。
func acceptedMaxChars(project story.Project) int {
	return project.ChapterMaxChars + project.ChapterMaxChars/10
}

// acceptedMinChars 允许 10% 的下浮，避免为几十个字符追加无效描写。
func acceptedMinChars(project story.Project) int {
	return project.ChapterMinChars - project.ChapterMinChars/10
}
