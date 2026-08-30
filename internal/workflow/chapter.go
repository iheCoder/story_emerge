package workflow

import (
	"context"
	"fmt"
	"strings"

	"story_emerge/internal/story"
)

// chapterDraftInput 把经过审核的章节计划和状态上下文交给写作者；
// writer 只负责把施工图写成正文，不负责改变世界状态。
type chapterDraftInput struct {
	Context       chapterContext    `json:"context"`
	Plan          story.ChapterPlan `json:"approved_plan"`
	LengthControl string            `json:"length_control"`
}

// recorderInput 让书记员以正文为事实来源提取 delta，而不是让写作者直接编辑状态 JSON。
type recorderInput struct {
	Context chapterContext    `json:"context"`
	Plan    story.ChapterPlan `json:"approved_plan"`
	Chapter string            `json:"chapter_text"`
}

// recorderRepairInput 在 ApplyDelta 拒绝后携带原始 delta 和明确错误，要求书记员只修状态归类。
type recorderRepairInput struct {
	Context         chapterContext    `json:"context"`
	Plan            story.ChapterPlan `json:"approved_plan"`
	Chapter         string            `json:"chapter_text"`
	RejectedDelta   story.StateDelta  `json:"rejected_delta"`
	ValidationError string            `json:"validation_error"`
	RepairRule      string            `json:"repair_rule"`
}

// editorInput 同时提供正文、计划和候选状态，编辑器因此能检查“写了什么”与“账本是否匹配”。
type editorInput struct {
	Context          chapterContext    `json:"context"`
	Plan             story.ChapterPlan `json:"approved_plan"`
	Chapter          string            `json:"chapter_text"`
	Delta            story.StateDelta  `json:"proposed_state_delta"`
	VisibleChars     int               `json:"exact_visible_chars"`
	AllowedCharRange string            `json:"allowed_char_range"`
}

// revisionInput 是受审核意见驱动的整章修订输入；二次局部纠错也复用它以保持规则一致。
type revisionInput struct {
	Context       chapterContext    `json:"context"`
	Plan          story.ChapterPlan `json:"approved_plan"`
	Chapter       string            `json:"current_chapter"`
	Review        story.Review      `json:"editor_review"`
	LengthControl string            `json:"length_control"`
}

// Run 从 HEAD 继续写作；limit=0 表示一直写到项目目标章节。
func (engine *Engine) Run(ctx context.Context, limit int) error {
	// 每次运行先从 HEAD 恢复三份基线，再按 remaining 次调用 runChapter。
	// limit 只限制本次前进步数，不会修改项目的 TargetChapters。
	// 从磁盘恢复项目、Bible 和 HEAD 状态。
	project, bible, state, err := engine.loadRunState()
	if err != nil {
		return err
	}

	// 计算本次运行允许推进的章节数；limit 不会改变全书目标。
	remaining := project.TargetChapters - state.Chapter
	if limit > 0 && limit < remaining {
		remaining = limit
	}

	// 逐章执行事务；只有成功提交的 next 才成为下一轮输入。
	for index := 0; index < remaining; index++ {
		// 每轮成功才更新内存中的 state；失败会停在上一章，下一次运行可安全重试。
		next, err := engine.runChapter(ctx, project, bible, state)
		if err != nil {
			return err
		}
		state = next
	}

	// 向 CLI 报告本次运行的最终提交位置。
	engine.emit("complete", fmt.Sprintf("已写到第 %d 章", state.Chapter))
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

// runChapter 尝试生成并提交一个章节；失败时返回错误且不推进正式状态。
func (engine *Engine) runChapter(
	ctx context.Context,
	project story.Project,
	bible story.StoryBible,
	current story.State,
) (story.State, error) {
	// 单章是“准备 -> 有界修订 -> 可选复盘 -> 提交”的事务边界。
	// 只有 review 通过且 CommitChapter 成功，状态才会对下一轮可见。
	// 确定下一章编号并生成正文/状态候选。
	number := current.Chapter + 1
	engine.emit("chapter", fmt.Sprintf("开始第 %d 章", number))
	working, err := engine.prepareChapter(ctx, project, bible, current)
	if err != nil {
		return story.State{}, err
	}

	// 硬伤触发一次整章修订，修订后会重新提取状态并审核。
	if !working.review.Passed {
		// 第一次审核失败才进入整章修订；通过的章节不做无谓重写以保留已验证的因果。
		working, err = engine.reviseChapter(ctx, project, working)
		if err != nil {
			return story.State{}, err
		}
	}

	// 仅对单个局部硬伤进行低温度纠错，避免结构性失败反复扩写。
	if canMinorCorrect(project, working) {
		// 满足“分数尚可、仅一个硬伤、长度已合格”才允许轻量纠错，
		// 将局部问题与结构性失败区分开，控制额外模型调用。
		working, err = engine.correctChapter(ctx, project, working)
		if err != nil {
			return story.State{}, err
		}
	}

	// 修订预算耗尽仍未通过时停止，不写入正式章节。
	if !working.review.Passed {
		return story.State{}, fmt.Errorf("第 %d 章经过有界修订后仍未通过，现场已保存在 .work", number)
	}

	// 在每四章或终章更新下一阶段指导。
	if err := engine.reflectArc(ctx, &working); err != nil {
		return story.State{}, err
	}

	// 所有质量门槛通过后原子提交章节及状态。
	if err := engine.store.CommitChapter(working.plan, working.chapter, working.delta, working.review, working.next); err != nil {
		return story.State{}, err
	}

	engine.emit("chapter", fmt.Sprintf("第 %d 章已提交，评分 %d", number, working.review.Score))
	return working.next, nil
}

// chapterWork 是单章尝试的内存工作集；其中 next 只有在最终提交后才成为正式状态。
type chapterWork struct {
	context chapterContext
	plan    story.ChapterPlan
	chapter string
	delta   story.StateDelta
	review  story.Review
	next    story.State
}

// prepareChapter 执行上下文、计划、正文和初次检查四个准备阶段。
func (engine *Engine) prepareChapter(
	ctx context.Context,
	project story.Project,
	bible story.StoryBible,
	current story.State,
) (chapterWork, error) {
	// prepareChapter 固定执行四个阶段：构建上下文、生成计划、写正文、提取并审核状态。
	// 每个中间产物都可保存到 .work，便于失败后判断卡在“写作”还是“记账”。
	// 根据 HEAD 构建共享上下文。
	chapterCtx, err := engine.buildChapterContext(project, bible, current)
	if err != nil {
		return chapterWork{}, err
	}

	// 生成并校验章节计划。
	plan, err := engine.planChapter(ctx, project, chapterCtx)
	if err != nil {
		return chapterWork{}, err
	}

	// 按已批准计划生成正文。
	chapter, err := engine.writeChapter(ctx, project, chapterCtx, plan)
	if err != nil {
		return chapterWork{}, err
	}

	// 保存原始草稿，并进入统一检查入口。
	work := chapterWork{context: chapterCtx, plan: plan, chapter: chapter}
	if err := engine.store.SaveWorking(plan.Number, "draft.md", chapter); err != nil {
		return chapterWork{}, err
	}
	return engine.inspectChapter(ctx, project, work, "initial")
}

// planChapter 生成并校验下一章的场景施工图。
func (engine *Engine) planChapter(
	ctx context.Context,
	project story.Project,
	chapterCtx chapterContext,
) (story.ChapterPlan, error) {
	// 计划先于正文生成并经过领域校验；没有至少三个场景和尾钩的计划不能进入 writer。
	// 把上下文编码成规划器输入。
	input, err := asPrettyJSON(chapterCtx)
	if err != nil {
		return story.ChapterPlan{}, err
	}

	// 调用规划器生成结构化计划。
	stage := chapterStage(chapterCtx.NextChapter, "plan")
	// 章节策划的输出结构已经足够约束，不需要额外长链推理。
	// 对 Flash 模型使用 none，避免推理 token 挤占最终 JSON 的输出空间。
	plan, err := generateJSON[story.ChapterPlan](ctx, engine, stage, "planner", "chapter_plan", input, 7000, "none")
	if err != nil {
		return story.ChapterPlan{}, err
	}

	// 验证章节号、字数目标、剧情线引用、场景数和尾钩。
	if err := validatePlan(project, chapterCtx.State, plan); err != nil {
		return story.ChapterPlan{}, fmt.Errorf("第 %d 章计划无效: %w", chapterCtx.NextChapter, err)
	}

	// 保存计划现场，供正文失败时人工复盘。
	if err := engine.store.SaveWorking(plan.Number, "plan.json", plan); err != nil {
		return story.ChapterPlan{}, err
	}
	return plan, nil
}

// writeChapter 按已批准计划生成正文，不直接修改状态账本。
func (engine *Engine) writeChapter(
	ctx context.Context,
	project story.Project,
	chapterCtx chapterContext,
	plan story.ChapterPlan,
) (string, error) {
	// writer 输入包含明确字数控制和已批准计划，避免它自行发明章节目标。
	// token 上限比字符窗口更底层，双重限制可同时防止截断和失控扩写。
	// 组合 Bible/状态上下文、已批准计划和双重长度控制。
	input, err := asPrettyJSON(chapterDraftInput{
		Context: chapterCtx, Plan: plan, LengthControl: lengthControl(project),
	})
	if err != nil {
		return "", err
	}

	// 以正文 token 上限调用 writer，防止响应被无界扩写或截断。
	maxTokens := chapterTextTokenLimit(project)
	return generateText(ctx, engine, chapterStage(plan.Number, "write"), "writer", input, maxTokens, 0.9)
}

// inspectChapter 从正文提取候选状态并执行编辑审核。
func (engine *Engine) inspectChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
	attempt string,
) (chapterWork, error) {
	// inspect 是每次正文尝试的统一质检入口：先提取可应用状态，再做模型审核和确定性硬检。
	// attempt 只影响 .work 文件名，不影响正式章节编号或状态语义。
	// 从正文提取并验证候选状态，得到不会破坏账本的 next。
	delta, next, err := engine.extractValidDelta(ctx, work)
	if err != nil {
		return work, err
	}

	// 让编辑器同时检查正文质量和正文/状态是否一致。
	review, err := engine.reviewChapter(ctx, project, work, delta)
	if err != nil {
		return work, err
	}

	// 合并长度、元文本和终章收束等程序硬检查，程序规则拥有否决权。
	review = mergeDeterministicIssues(review, terminalClosureIssues(project, next))

	// 更新本次工作集并保存对应尝试的审计文件。
	work.delta, work.next, work.review = delta, next, review
	if err := engine.saveInspection(work.plan.Number, attempt, delta, review); err != nil {
		return work, err
	}
	return work, nil
}

// extractValidDelta 提取、归一化并验证本章状态变化，必要时只修复一次；
// 正文和章节计划在此阶段保持不变。
func (engine *Engine) extractValidDelta(
	ctx context.Context,
	work chapterWork,
) (story.StateDelta, story.State, error) {
	// 书记员输出先归一化再 ApplyDelta；若引用校验失败，只允许一次针对错误的状态修复。
	// 正文和章节计划在整个过程中保持不变，避免“为了让账本通过而改写事实来源”。
	// 让书记员从当前正文提取 StateDelta。
	delta, err := engine.extractDelta(ctx, work)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	// 删除临时人物/未知关系等不会进入长期账本的字段。
	delta, err = engine.normalizeDelta(work.plan.Number, work.context.State, delta)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	// 尝试将规范化 delta 应用到旧状态。
	next, validationErr := story.ApplyDelta(work.context.State, delta)
	if validationErr == nil {
		return delta, next, nil
	}

	// 保存被拒 delta 供人工定位；修复请求拿到同一正文和错误信息，不能凭空重建状态。
	// 保存被拒版本，并把明确校验错误交给一次修复调用。
	if err := engine.store.SaveWorking(work.plan.Number, "delta-invalid.json", delta); err != nil {
		return story.StateDelta{}, story.State{}, err
	}
	repaired, err := engine.repairDelta(ctx, work, delta, validationErr)
	if err != nil {
		return story.StateDelta{}, story.State{}, err
	}

	// 对修复结果再次做同样的归一化和领域应用；第二次失败直接终止。
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

// normalizeDelta 删除无法进入长期账本的临时引用，并保存诊断信息。
func (engine *Engine) normalizeDelta(
	number int,
	current story.State,
	delta story.StateDelta,
) (story.StateDelta, error) {
	// 模型可能把一次性人物、未知关系或新线更新重复写入长期账本；
	// 先做安全删减，再把删减项记录为诊断，不让脏字段阻塞整章。
	// 依次清理未知人物、未知关系和重复新剧情线更新。
	normalized, characters := story.DropUnknownCharacterStates(current, delta)
	normalized, relationships := story.DropUnknownRelationships(current, normalized)
	normalized, threads := story.DropDuplicatedNewThreadUpdates(current, normalized)

	// 没有发生清理时直接返回，避免额外写诊断文件和进度噪声。
	if len(characters)+len(relationships)+len(threads) == 0 {
		return normalized, nil
	}

	// 保存清理项并通知调用方，保留模型输出与正典状态之间的差异证据。
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

// saveInspection 将某次审核尝试的 delta 与 review 成对写入 .work。
func (engine *Engine) saveInspection(
	number int,
	attempt string,
	delta story.StateDelta,
	review story.Review,
) error {
	// 每次审核尝试保存 delta 和 review 配对文件，便于比较 initial/revised/corrected 的变化。
	// 先保存状态增量；失败时不继续写审核文件，避免出现无法配对的现场。
	if err := engine.store.SaveWorking(number, "delta-"+attempt+".json", delta); err != nil {
		return err
	}

	// 再保存同一次尝试的审核结果。
	return engine.store.SaveWorking(number, "review-"+attempt+".json", review)
}

// extractDelta 调用书记员把正文事实投影为机器可校验的 StateDelta。
func (engine *Engine) extractDelta(ctx context.Context, work chapterWork) (story.StateDelta, error) {
	// 书记员只看当前上下文、计划和正文，并通过 Schema 输出可机器验证的 StateDelta。
	// 准备书记员的上下文、计划和正文输入。
	input, err := asPrettyJSON(recorderInput{Context: work.context, Plan: work.plan, Chapter: work.chapter})
	if err != nil {
		return story.StateDelta{}, err
	}

	// 执行结构化记录调用。
	stage := chapterStage(work.plan.Number, "record")
	return generateJSON[story.StateDelta](ctx, engine, stage, "recorder", "state_delta", input, 6500, "none")
}

// repairDelta 将领域校验错误反馈给书记员，只修复状态字段而不改正文。
func (engine *Engine) repairDelta(
	ctx context.Context,
	work chapterWork,
	rejected story.StateDelta,
	validationErr error,
) (story.StateDelta, error) {
	// repair 请求明确“只改状态 JSON”，把领域校验错误原样提供给模型，减少重复犯错。
	// 组合原始 delta、校验错误和只修状态的规则。
	input, err := asPrettyJSON(recorderRepairInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter,
		RejectedDelta: rejected, ValidationError: validationErr.Error(),
		RepairRule: "只修复状态 JSON；knowledge 与 relationships 只写本章增量，旧条目由程序自动保留；不得改变正文已经发生的事实",
	})
	if err != nil {
		return story.StateDelta{}, err
	}

	// 执行一次有限的状态修复调用。
	stage := chapterStage(work.plan.Number, "record_repair")
	return generateJSON[story.StateDelta](ctx, engine, stage, "recorder", "state_delta", input, 6500, "none")
}

// reviewChapter 调用编辑器并叠加确定性长度/元文本检查。
func (engine *Engine) reviewChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
	delta story.StateDelta,
) (story.Review, error) {
	// 编辑器看到精确可见字数和允许窗口；随后再叠加程序硬检，防止模型高估中文长度或漏报元文本。
	// 准备包含正文、计划、候选状态和精确字数的编辑器输入。
	input, err := asPrettyJSON(editorInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Delta: delta,
		VisibleChars:     visibleRuneCount(work.chapter),
		AllowedCharRange: fmt.Sprintf("%d-%d", acceptedMinChars(project), acceptedMaxChars(project)),
	})
	if err != nil {
		return story.Review{}, err
	}

	// 调用编辑器生成结构化审核意见。
	stage := chapterStage(work.plan.Number, "review")
	review, err := generateJSON[story.Review](ctx, engine, stage, "editor", "review", input, 6500, "low")
	if err != nil {
		return story.Review{}, err
	}

	// 叠加程序确定性检查，避免模型漏报标题、长度或元文本问题。
	issues := deterministicIssues(work.chapter, acceptedMinChars(project), acceptedMaxChars(project))
	return mergeDeterministicIssues(review, issues), nil
}

// reviseChapter 按审核意见整章重写，然后重新走状态提取和审核。
func (engine *Engine) reviseChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
) (chapterWork, error) {
	// 整章修订替换正文后必须重新走 extract/normalize/review，旧 delta 不能沿用到新正文。
	// 把当前正文和审核动作编码为修订输入。
	input, err := asPrettyJSON(revisionInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Review: work.review,
		LengthControl: lengthControl(project),
	})
	if err != nil {
		return work, err
	}

	// 以较低温度整章重写，优先修复硬伤并保持计划/字数约束。
	maxTokens := chapterTextTokenLimit(project)
	stage := chapterStage(work.plan.Number, "revise")
	chapter, err := generateText(ctx, engine, stage, "revision", input, maxTokens, 0.8)
	if err != nil {
		return work, err
	}

	// 替换草稿并重新进入 inspect，禁止沿用旧 delta/review。
	work.chapter = chapter
	if err := engine.store.SaveWorking(work.plan.Number, "revised.md", chapter); err != nil {
		return work, err
	}
	return engine.inspectChapter(ctx, project, work, "revised")
}

// correctChapter 对二审后剩下的单个局部硬伤做低温度二次修订，不承担结构性重写。
func (engine *Engine) correctChapter(
	ctx context.Context,
	project story.Project,
	work chapterWork,
) (chapterWork, error) {
	// 轻量纠错同样重新审核，但把温度降到 0.4，尽量只修一个已定位硬伤而不引入新剧情。
	// 复用审核上下文，准备局部纠错输入。
	input, err := asPrettyJSON(revisionInput{
		Context: work.context, Plan: work.plan, Chapter: work.chapter, Review: work.review,
		LengthControl: lengthControl(project),
	})
	if err != nil {
		return work, err
	}

	// 用更低温度执行一次局部修订，降低引入新剧情的概率。
	stage := chapterStage(work.plan.Number, "correct")
	chapter, err := generateText(ctx, engine, stage, "revision", input, chapterTextTokenLimit(project), 0.4)
	if err != nil {
		return work, err
	}

	// 保存纠错稿并重新检查，确认修复没有制造新的硬伤。
	work.chapter = chapter
	if err := engine.store.SaveWorking(work.plan.Number, "corrected.md", chapter); err != nil {
		return work, err
	}

	return engine.inspectChapter(ctx, project, work, "corrected")
}

// canMinorCorrect 判断当前失败是否适合进入一次局部纠错。
func canMinorCorrect(project story.Project, work chapterWork) bool {
	// 这是“是否值得再调用一次模型”的决策门：质量尚可且长度合格才允许局部修复。
	count := visibleRuneCount(work.chapter)
	return !work.review.Passed && work.review.Score >= 70 && len(work.review.HardIssues) == 1 &&
		count >= acceptedMinChars(project) && count <= acceptedMaxChars(project)
}

// reflectArc 在阶段节点调用复盘角色，更新下一阶段写作指导。
func (engine *Engine) reflectArc(ctx context.Context, work *chapterWork) error {
	// 每四章及终章做一次阶段复盘；复盘只更新 DirectorGuidance，不能直接篡改已提交事实。
	// 判断当前章节是否是每四章或终章复盘节点。
	if work.next.Chapter%4 != 0 && work.next.Chapter != work.context.TargetChapters {
		// 非复盘节点跳过模型调用，节省预算并保持章节节奏稳定。
		return nil
	}

	// 用候选状态而非旧状态准备复盘输入。
	input, err := asPrettyJSON(struct {
		Context chapterContext `json:"context"`
		State   story.State    `json:"candidate_state"`
	}{Context: work.context, State: work.next})
	if err != nil {
		return err
	}

	// 调用复盘角色生成下一阶段优先级和警告。
	stage := chapterStage(work.next.Chapter, "arc_review")
	reflection, err := generateJSON[story.ArcReflection](ctx, engine, stage, "arc_review", "arc_review", input, 5500, "low")
	if err != nil {
		return err
	}

	// 把复盘结果投影为下一章指导，不修改历史事实或人物状态。
	work.next.DirectorGuidance = append([]string(nil), reflection.Priorities...)
	for _, warning := range reflection.Warnings {
		work.next.DirectorGuidance = append(work.next.DirectorGuidance, "避免："+warning)
	}

	// 保存复盘原文，供人工审阅本阶段节奏判断。
	return engine.store.SaveWorking(work.next.Chapter, "arc-review.json", reflection)
}

// validatePlan 校验章节计划是否符合项目范围和当前账本。
func validatePlan(project story.Project, state story.State, plan story.ChapterPlan) error {
	// 计划校验只依赖当前状态和项目契约：章节号、字数目标、剧情线白名单、场景数和尾钩。
	// 确认计划编号和目标字数属于当前项目窗口。
	if plan.Number != state.Chapter+1 {
		return fmt.Errorf("章节号应为 %d", state.Chapter+1)
	}
	if plan.TargetChars < project.ChapterMinChars || plan.TargetChars > project.ChapterMaxChars {
		return fmt.Errorf("目标字数 %d 不在项目范围内", plan.TargetChars)
	}

	// 建立当前剧情线白名单并检查计划引用。
	known := make(map[string]bool, len(state.Threads))
	for _, thread := range state.Threads {
		known[thread.ID] = true
	}
	for _, threadID := range plan.ActiveThreads {
		if !known[threadID] {
			return fmt.Errorf("引用未知剧情线: %s", threadID)
		}
	}

	// 确认章节具备至少三个场景和明确尾钩。
	if len(plan.Scenes) < 3 || strings.TrimSpace(plan.Hook) == "" {
		return fmt.Errorf("至少需要三个场景和明确尾钩")
	}
	return nil
}

// chapterStage 生成可用于日志、用量和现场文件的稳定阶段名。
func chapterStage(number int, action string) string {
	// 统一阶段命名让 usage.jsonl、进度事件和 .work 文件可以按章节自然聚合。
	return fmt.Sprintf("chapter_%03d_%s", number, action)
}

// lengthControl 生成同时约束字符数和 token 数的写作提示。
func lengthControl(project story.Project) string {
	// 同时给出字符窗口和近似 token 窗口，提醒模型 token 与中文字符不是一比一关系。
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
	// 取最大下限 1800 保障短项目仍能写出完整场景；正常项目按字符上限预留 1/7 余量。
	return max(1800, project.ChapterMaxChars*6/7)
}

// acceptedMaxChars 给生成文本保留 10% 的自然波动，但写作 Prompt 仍以项目上限为目标。
// 这避免为了几十个字符反复整章重写，同时又不会接受明显失控的篇幅。
func acceptedMaxChars(project story.Project) int {
	// 审核允许小幅自然波动，但硬上限仍保留，避免模型输出数量级失控的长文。
	return project.ChapterMaxChars + project.ChapterMaxChars/10
}

// acceptedMinChars 允许 10% 的下浮，避免为几十个字符追加无效描写。
func acceptedMinChars(project story.Project) int {
	// 对最小字数同样给 10% 容差，避免为了几个字符添加没有叙事功能的水句。
	return project.ChapterMinChars - project.ChapterMinChars/10
}
