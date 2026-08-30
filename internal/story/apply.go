package story

import "fmt"

// NewInitialState 把总导演产生的初始素材转换成第 0 章的正式快照。
func NewInitialState(initial InitialState) State {
	return State{
		Chapter:          0,
		Characters:       clone(initial.Characters),
		Facts:            clone(initial.Facts),
		Threads:          clone(initial.Threads),
		Timeline:         []TimelineEvent{},
		Summaries:        []ChapterSummary{},
		DirectorGuidance: clone(initial.DirectorGuidance),
	}
}

// ApplyDelta 先验证、后复制、再应用，保证失败不会改坏调用方持有的旧状态。
func ApplyDelta(current State, delta StateDelta) (State, error) {
	if err := validateDelta(current, delta); err != nil {
		return State{}, err
	}
	next := cloneState(current)
	next.Chapter = delta.Chapter
	next.Facts = append(next.Facts, clone(delta.NewFacts)...)
	next.Timeline = append(next.Timeline, clone(delta.TimelineEvents)...)
	next.Summaries = append(next.Summaries, delta.Summary)
	next.Characters = mergeCharacters(next.Characters, delta.CharacterStates)
	next.Threads = replaceThreads(next.Threads, delta.ThreadStates)
	next.Threads = append(next.Threads, clone(delta.NewThreads)...)
	return next, nil
}

// DropUnknownRelationships 清理模型为临时人物创建的长期关系。
// 临时人物仍可保留在时间线文字中，但不会因此无限扩张主要人物状态表。
func DropUnknownRelationships(current State, delta StateDelta) (StateDelta, []string) {
	known := make(map[string]bool, len(current.Characters))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}
	delta.CharacterStates = clone(delta.CharacterStates)
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
	known := make(map[string]bool, len(current.Characters))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}
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
	known := make(map[string]bool, len(current.Threads))
	for _, thread := range current.Threads {
		known[thread.ID] = true
	}
	created := make(map[string]bool, len(delta.NewThreads))
	for _, thread := range delta.NewThreads {
		created[thread.ID] = true
	}
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

func validateDelta(current State, delta StateDelta) error {
	expected := current.Chapter + 1
	if delta.Chapter != expected || delta.Summary.Number != expected {
		return fmt.Errorf("状态变化章节应为 %d，实际为 %d/%d", expected, delta.Chapter, delta.Summary.Number)
	}
	if err := validateFactChanges(current, delta.NewFacts); err != nil {
		return err
	}
	if err := validateCharacterChanges(current, delta.NewFacts, delta.CharacterStates); err != nil {
		return err
	}
	return validateThreadChanges(current, delta.ThreadStates, delta.NewThreads)
}

func validateFactChanges(current State, additions []Fact) error {
	known := make(map[string]bool, len(current.Facts)+len(additions))
	for _, fact := range current.Facts {
		known[fact.ID] = true
	}
	for _, fact := range additions {
		if fact.ID == "" || known[fact.ID] {
			return fmt.Errorf("新增事实 ID 为空或重复: %s", fact.ID)
		}
		known[fact.ID] = true
	}
	return nil
}

func validateCharacterChanges(current State, newFacts []Fact, changes []CharacterState) error {
	if len(changes) > 4 {
		return fmt.Errorf("单章人物状态更新过多: %d，最多允许 4 个", len(changes))
	}
	known := make(map[string]bool, len(current.Characters))
	facts := make(map[string]bool, len(current.Facts)+len(newFacts))
	for _, character := range current.Characters {
		known[character.CharacterID] = true
	}
	for _, fact := range append(current.Facts, newFacts...) {
		facts[fact.ID] = true
	}
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

func validateThreadChanges(current State, updates, additions []PlotThreadState) error {
	known := make(map[string]bool, len(current.Threads)+len(additions))
	for _, thread := range current.Threads {
		known[thread.ID] = true
	}
	for _, update := range updates {
		if !known[update.ID] || !validThreadStatus[update.Status] {
			return fmt.Errorf("剧情线更新无效: %s/%s", update.ID, update.Status)
		}
	}
	for _, addition := range additions {
		if addition.ID == "" || known[addition.ID] || !validThreadStatus[addition.Status] {
			return fmt.Errorf("新增剧情线无效: %s/%s", addition.ID, addition.Status)
		}
		known[addition.ID] = true
	}
	return nil
}

func mergeCharacters(current, changes []CharacterState) []CharacterState {
	byID := make(map[string]CharacterState, len(changes))
	for _, change := range changes {
		byID[change.CharacterID] = change
	}
	for index, character := range current {
		if change, ok := byID[character.CharacterID]; ok {
			change.Knowledge = mergeKnowledge(character.Knowledge, change.Knowledge)
			change.Relationships = mergeRelationships(character.Relationships, change.Relationships)
			current[index] = change
		}
	}
	return current
}

func mergeKnowledge(current, changes []Knowledge) []Knowledge {
	result := clone(current)
	positions := make(map[string]int, len(result))
	for index, item := range result {
		positions[item.FactID] = index
	}
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

func mergeRelationships(current, changes []Relationship) []Relationship {
	result := clone(current)
	positions := make(map[string]int, len(result))
	for index, item := range result {
		positions[item.TargetID] = index
	}
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

func replaceThreads(current, changes []PlotThreadState) []PlotThreadState {
	byID := make(map[string]PlotThreadState, len(changes))
	for _, change := range changes {
		byID[change.ID] = change
	}
	for index, thread := range current {
		if replacement, ok := byID[thread.ID]; ok {
			current[index] = replacement
		}
	}
	return current
}

func cloneState(state State) State {
	state.Characters = clone(state.Characters)
	state.Facts = clone(state.Facts)
	state.Threads = clone(state.Threads)
	state.Timeline = clone(state.Timeline)
	state.Summaries = clone(state.Summaries)
	state.DirectorGuidance = clone(state.DirectorGuidance)
	return state
}

func clone[T any](source []T) []T {
	return append([]T(nil), source...)
}
