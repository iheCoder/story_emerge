package story

import "fmt"

// NewInitialState 把 Architect 一次性产生的初始素材转换成第 0 章正式快照。
func NewInitialState(initial InitialState, outline StoryOutline) State {
	// 第 0 章没有正文和时间线。复制 Architect 交付的事实、人物和剧情线，
	// 既为后续 ApplyDelta 提供稳定基线，也避免 append 修改 Genesis 的底层切片。
	return State{
		Chapter:         0,
		Characters:      clone(initial.Characters),
		Facts:           clone(initial.Facts),
		Threads:         clone(initial.Threads),
		Timeline:        []TimelineEvent{},
		Summaries:       []ChapterSummary{},
		OutlineVersion:  outline.Version,
		OutlineProgress: OutlineProgress{CurrentMovementID: outline.CurrentMovementID, Status: "ongoing"},
		StoryStatus:     "ongoing",
	}
}

// ApplyDelta 先验证、后复制、再应用，保证失败不会改坏调用方持有的旧状态。
func ApplyDelta(current State, delta StateDelta) (State, error) {
	// 顺序必须保持为 validate -> clone -> append/merge：
	// 校验阶段不触碰输入，复制阶段隔离切片底层数组，应用阶段才产生新快照。
	// 校验章节编号和所有跨表引用。
	if err := validateDelta(current, delta); err != nil {
		return State{}, err
	}

	// 复制当前快照，保证调用方仍可使用旧状态重试或审计。
	next := cloneState(current)

	// 追加本章事实、时间线和摘要。
	next.Chapter = delta.Chapter
	next.Facts = append(next.Facts, clone(delta.NewFacts)...)
	next.Timeline = append(next.Timeline, clone(delta.TimelineEvents)...)
	next.Summaries = append(next.Summaries, delta.Summary)

	// 按稳定 ID 合并人物动态和知识关系。
	next.Characters = mergeCharacters(next.Characters, delta.CharacterStates)

	// 替换既有剧情线并追加新线，形成下一章唯一状态快照。
	next.Threads = replaceThreads(next.Threads, delta.ThreadStates)
	next.Threads = append(next.Threads, clone(delta.NewThreads)...)
	next.OutlineProgress = delta.OutlineProgress
	next.StoryStatus = delta.StoryStatus
	if delta.OutlineProgress.Status == "completed" {
		next.CompletedMovementIDs = appendUnique(next.CompletedMovementIDs, delta.OutlineProgress.CurrentMovementID)
	}
	return next, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// DropUnknownRelationships 清理模型为临时人物创建的长期关系。
// 临时人物仍可保留在时间线文字中，但不会因此无限扩张主要人物状态表。
func DropUnknownRelationships(current State, delta StateDelta) (StateDelta, []string) {
	// 先建立当前正式人物索引，再逐个人物过滤关系；返回 dropped 供审核日志说明“删了什么、为何删”。
	// 建立当前正式人物白名单。
	known := make(map[string]bool, len(current.Characters))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}

	// 复制人物增量，确保清理不会修改模型解码出的原始值。
	delta.CharacterStates = clone(delta.CharacterStates)

	// 过滤未知关系并收集诊断 ID。
	var dropped []string
	for index, character := range delta.CharacterStates {
		kept := make([]Relationship, 0, len(character.Relationships))
		for _, relation := range character.Relationships {
			if known[relation.TargetID] {
				kept = append(kept, relation)
				continue
			}
			dropped = append(dropped, fmt.Sprintf("%s->%s", character.CharacterID, relation.TargetID))
		}
		delta.CharacterStates[index].Relationships = kept
	}
	return delta, dropped
}

// DropUnknownCharacterStates 防止一次性线索人物进入长期人物快照。
// 他们的行动仍保留在事实与时间线中，不会因此丢失剧情历史。
func DropUnknownCharacterStates(current State, delta StateDelta) (StateDelta, []string) {
	// 只清理长期状态引用，不触碰 NewFacts/TimelineEvents，确保一次性人物的戏份仍可在历史中追溯。
	// 建立正式人物白名单。
	known := make(map[string]bool, len(current.Characters))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}

	// 只保留正式人物状态；临时人物仍由 facts/timeline 记录历史。
	kept := delta.CharacterStates[:0]
	var dropped []string
	for _, change := range delta.CharacterStates {
		if !known[change.CharacterID] {
			dropped = append(dropped, change.CharacterID)
			continue
		}
		kept = append(kept, change)
	}
	delta.CharacterStates = kept
	return delta, dropped
}

// DropDuplicatedNewThreadUpdates 修正书记员常见的字段归类错误：
// 同一条新剧情线同时出现在 thread_states 与 new_threads。
// new_threads 已含完整定义，因此删除无效的“既有线更新”不会改变业务语义。
func DropDuplicatedNewThreadUpdates(current State, delta StateDelta) (StateDelta, []string) {
	// 通过“当前已知剧情线”和“本章新建剧情线”两张索引判定重复归类，
	// 只删除不可能生效的更新项，保留完整的 NewThreads 定义。
	// 分别建立既有剧情线和本章新建剧情线索引。
	known := make(map[string]bool, len(current.Threads))
	for _, thread := range current.Threads {
		known[thread.ID] = true
	}
	created := make(map[string]bool, len(delta.NewThreads))
	for _, thread := range delta.NewThreads {
		created[thread.ID] = true
	}

	// 删除“既有线更新”中的重复新线，并保留完整 NewThreads 定义。
	kept := delta.ThreadStates[:0]
	var dropped []string
	for _, update := range delta.ThreadStates {
		if !known[update.ID] && created[update.ID] {
			dropped = append(dropped, update.ID)
			continue
		}
		kept = append(kept, update)
	}
	delta.ThreadStates = kept
	return delta, dropped
}

// validateDelta 校验章节连续性以及 delta 对事实、人物和剧情线的所有引用。
func validateDelta(current State, delta StateDelta) error {
	// 先检查章节编号，再按事实→人物→剧情线的依赖顺序校验引用，
	// 这样错误信息通常能指向最早的违规字段，而不是后续连锁失败。
	// 确认 delta 正好推进一个章节，并与摘要编号一致。
	expected := current.Chapter + 1
	if delta.Chapter != expected || delta.Summary.Number != expected {
		return fmt.Errorf("状态变化章节应为 %d，实际为 %d/%d", expected, delta.Chapter, delta.Summary.Number)
	}
	if !validProgressStatus[delta.OutlineProgress.Status] || delta.OutlineProgress.CurrentMovementID == "" {
		return fmt.Errorf("大纲推进状态无效: %s/%s", delta.OutlineProgress.CurrentMovementID, delta.OutlineProgress.Status)
	}
	if delta.OutlineProgress.CurrentMovementID != current.OutlineProgress.CurrentMovementID {
		return fmt.Errorf("书记员不能切换当前故事阶段")
	}
	if !validStoryStatus[delta.StoryStatus] {
		return fmt.Errorf("全书状态无效: %s", delta.StoryStatus)
	}
	if current.StoryStatus == "ending" && delta.StoryStatus == "ongoing" {
		return fmt.Errorf("全书已进入收束阶段，不能退回 ongoing")
	}
	if current.StoryStatus == "completed" && delta.StoryStatus != "completed" {
		return fmt.Errorf("已完成故事不能重新开启")
	}

	// 校验新增事实的唯一性。
	if err := validateFactChanges(current, delta.NewFacts); err != nil {
		return err
	}

	// 校验人物状态及其事实/关系引用。
	if err := validateCharacterChanges(current, delta.NewFacts, delta.CharacterStates); err != nil {
		return err
	}

	// 校验既有剧情线更新和新剧情线定义。
	return validateThreadChanges(current, delta.ThreadStates, delta.NewThreads)
}

// validateFactChanges 校验新增事实的 ID 在历史和本章范围内唯一。
func validateFactChanges(current State, additions []Fact) error {
	// 新事实 ID 必须在“历史事实 + 本章新增”全集中唯一；否则后续 Knowledge 无法稳定指向事实。

	// 把历史事实加入索引。
	known := make(map[string]bool, len(current.Facts)+len(additions))
	for _, fact := range current.Facts {
		known[fact.ID] = true
	}

	// 检查本章新增并即时登记，捕获同一 delta 内的重复。
	for _, fact := range additions {
		if fact.ID == "" || known[fact.ID] {
			return fmt.Errorf("新增事实 ID 为空或重复: %s", fact.ID)
		}
		known[fact.ID] = true
	}
	return nil
}

// validateCharacterChanges 校验人物白名单以及知识、关系引用。
func validateCharacterChanges(current State, newFacts []Fact, changes []CharacterState) error {
	// 只限制引用是否合法，不限制本章可以影响多少正式人物。人物数量是
	// 正文内容，不是程序应预设的节奏配额。
	known := make(map[string]bool, len(current.Characters))
	facts := make(map[string]bool, len(current.Facts)+len(newFacts))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}
	for _, fact := range append(current.Facts, newFacts...) {
		facts[fact.ID] = true
	}

	// 逐条检查人物 ID、知识关系引用和重复更新。
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if !known[change.CharacterID] || seen[change.CharacterID] {
			return fmt.Errorf("人物状态引用未知人物: %s", change.CharacterID)
		}
		if err := validateCharacterReferences(change, known, facts); err != nil {
			return err
		}
		seen[change.CharacterID] = true
	}
	return nil
}

// validateThreadChanges 区分既有剧情线更新与新线创建，并检查生命周期状态。
func validateThreadChanges(current State, updates, additions []PlotThreadState) error {
	// 更新只能作用于既有剧情线；新增线必须使用全新 ID 和受控状态，
	// 以保证 replaceThreads/append 的语义不会产生重复主线。
	// 登记当前账本中的既有剧情线。
	known := make(map[string]bool, len(current.Threads)+len(additions))
	for _, thread := range current.Threads {
		known[thread.ID] = true
	}

	// 更新只能命中既有 ID，且状态必须在生命周期白名单中。
	for _, update := range updates {
		if !known[update.ID] || !validThreadStatus[update.Status] {
			return fmt.Errorf("剧情线更新无效: %s/%s", update.ID, update.Status)
		}
	}

	// 新线必须使用新 ID，并在登记后参与后续重复检查。
	for _, addition := range additions {
		if addition.ID == "" || known[addition.ID] || !validThreadStatus[addition.Status] {
			return fmt.Errorf("新增剧情线无效: %s/%s", addition.ID, addition.Status)
		}
		known[addition.ID] = true
	}
	return nil
}

// mergeCharacters 按人物 ID 替换动态字段，并保留未在本章更新的长期认知/关系。
func mergeCharacters(current, changes []CharacterState) []CharacterState {
	// 人物主记录按 CharacterID 替换动态字段，但知识和关系按各自稳定键增量合并，
	// 防止“本章只更新一条关系”时误删掉其他长期记忆。
	// 建立本章人物变更索引。
	byID := make(map[string]CharacterState, len(changes))
	for _, change := range changes {
		byID[change.CharacterID] = change
	}

	// 遍历当前人物，命中变更时合并长期子集合。
	for index, character := range current {
		if change, ok := byID[character.CharacterID]; ok {
			change.Knowledge = mergeKnowledge(character.Knowledge, change.Knowledge)
			change.Relationships = mergeRelationships(character.Relationships, change.Relationships)
			current[index] = change
		}
	}
	return current
}

// mergeKnowledge 按 FactID 幂等合并人物知识。
func mergeKnowledge(current, changes []Knowledge) []Knowledge {
	// FactID 是知识条目的幂等键：同一事实更新相信程度，不同事实追加到末尾保持可读顺序。
	// 复制当前知识并建立 FactID 到位置的索引。
	result := clone(current)
	positions := make(map[string]int, len(result))
	for index, item := range result {
		positions[item.FactID] = index
	}

	// 已有事实原位更新，新事实追加，保持历史顺序。
	for _, change := range changes {
		if index, ok := positions[change.FactID]; ok {
			result[index] = change
		} else {
			positions[change.FactID] = len(result)
			result = append(result, change)
		}
	}
	return result
}

// mergeRelationships 按 TargetID 幂等合并有向人物关系。
func mergeRelationships(current, changes []Relationship) []Relationship {
	// TargetID 是有向关系的幂等键；更新关系属性而不是追加重复边，避免上下文越来越嘈杂。
	// 复制当前关系并建立 TargetID 到位置的索引。
	result := clone(current)
	positions := make(map[string]int, len(result))
	for index, item := range result {
		positions[item.TargetID] = index
	}

	// 已有关系原位更新，新关系追加。
	for _, change := range changes {
		if index, ok := positions[change.TargetID]; ok {
			result[index] = change
		} else {
			positions[change.TargetID] = len(result)
			result = append(result, change)
		}
	}
	return result
}

// replaceThreads 按剧情线 ID 替换已有记录，不负责追加新线。
func replaceThreads(current, changes []PlotThreadState) []PlotThreadState {
	// 剧情线更新采用整条记录替换，便于同时推进状态、进度和预定回收章节。
	// 建立本章剧情线更新索引。
	byID := make(map[string]PlotThreadState, len(changes))
	for _, change := range changes {
		byID[change.ID] = change
	}

	// 只替换已存在的剧情线；新增线由调用方单独 append。
	for index, thread := range current {
		if replacement, ok := byID[thread.ID]; ok {
			current[index] = replacement
		}
	}
	return current
}

// cloneState 复制 State 的顶层集合，供 ApplyDelta 构造不可破坏旧快照的新版本。
func cloneState(state State) State {
	// 复制所有切片字段；State 内含嵌套切片的类型目前按值存储，
	// 这里先隔离顶层数组，保证 append 不会改写 current 的共享容量。
	state.Characters = clone(state.Characters)
	state.Facts = clone(state.Facts)
	state.Threads = clone(state.Threads)
	state.Timeline = clone(state.Timeline)
	state.Summaries = clone(state.Summaries)
	state.CompletedMovementIDs = clone(state.CompletedMovementIDs)
	return state
}

// clone 返回 source 的新顶层切片；元素是值类型时不会共享数组容量。
func clone[T any](source []T) []T {
	// 统一的浅切片复制器用于值类型集合；调用方在合并前会对需要深层修改的嵌套字段再次复制。
	return append([]T(nil), source...)
}
