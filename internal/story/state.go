package story

import "fmt"

const (
	durableUpsert = "upsert"
	durableRemove = "remove"
)

// NewInitialState 把 Architect 的初始当前态复制为第 0 章快照。
func NewInitialState(initial InitialState, outline StoryOutline) State {
	return State{
		Chapter:         0,
		CharacterStates: clone(initial.CharacterStates),
		DurableStates:   clone(initial.DurableStates),
		TrackProgress:   clone(initial.TrackProgress),
		OutlineVersion:  outline.Version,
		StoryStatus:     "ongoing",
	}
}

// ApplyStoryUpdate 先完成全部确定性校验，再在副本上应用最终正文的长期变化。
// 被 Editor 否决的草稿不会调用本方法，因此不能污染正式状态。
func ApplyStoryUpdate(current State, outline StoryOutline, update StoryUpdate) (State, error) {
	if err := ValidateStoryUpdate(current, outline, update); err != nil {
		return State{}, err
	}

	next := cloneState(current)
	next.Chapter = update.Chapter
	next.CharacterStates = applyCharacterChanges(next.CharacterStates, update.CharacterChanges)
	next.DurableStates = applyDurableChanges(next.DurableStates, update.DurableStateChanges)
	next.TrackProgress = applyTrackChanges(next.TrackProgress, update.TrackChanges)
	next.OutlineVersion = outline.Version
	next.StoryStatus = update.StoryStatus

	return next, nil
}

// ValidateStoryUpdate 只保护章节、引用、ID 和状态枚举，不评价文学内容。
func ValidateStoryUpdate(current State, outline StoryOutline, update StoryUpdate) error {
	if update.Chapter != current.Chapter+1 {
		return fmt.Errorf("Story Update 章节应为 %d，实际为 %d", current.Chapter+1, update.Chapter)
	}
	if !validStoryStatus[update.StoryStatus] {
		return fmt.Errorf("故事状态无效: %s", update.StoryStatus)
	}
	if current.StoryStatus == "completed" && update.StoryStatus != "completed" {
		return fmt.Errorf("已完成故事不能重新开启")
	}

	if err := validateCharacterChanges(current, update.CharacterChanges); err != nil {
		return err
	}
	if err := validateDurableChanges(current, update.DurableStateChanges); err != nil {
		return err
	}

	return validateTrackChanges(outline, update.TrackChanges)
}

func validateCharacterChanges(current State, changes []CharacterStateChange) error {
	known := make(map[string]bool, len(current.CharacterStates))
	for _, character := range current.CharacterStates {
		known[character.CharacterID] = true
	}

	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if !known[change.CharacterID] || seen[change.CharacterID] {
			return fmt.Errorf("人物状态引用未知或重复人物: %s", change.CharacterID)
		}
		if change.State == "" {
			return fmt.Errorf("人物 %s 的长期状态为空", change.CharacterID)
		}
		seen[change.CharacterID] = true
	}

	return nil
}

func validateDurableChanges(current State, changes []DurableStateChange) error {
	known := make(map[string]bool, len(current.DurableStates))
	for _, state := range current.DurableStates {
		known[state.ID] = true
	}

	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if change.ID == "" || seen[change.ID] {
			return fmt.Errorf("长期状态 ID 为空或重复: %s", change.ID)
		}
		if change.Operation == durableRemove && !known[change.ID] {
			return fmt.Errorf("不能删除不存在的长期状态: %s", change.ID)
		}
		if change.Operation == durableUpsert && change.Description == "" {
			return fmt.Errorf("长期状态 %s 的描述为空", change.ID)
		}
		if change.Operation != durableUpsert && change.Operation != durableRemove {
			return fmt.Errorf("长期状态 %s 的操作无效: %s", change.ID, change.Operation)
		}
		seen[change.ID] = true
	}

	return nil
}

func validateTrackChanges(outline StoryOutline, changes []TrackProgressChange) error {
	known := make(map[string]bool, len(outline.Tracks))
	for _, track := range outline.Tracks {
		known[track.ID] = true
	}

	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if !known[change.TrackID] || seen[change.TrackID] {
			return fmt.Errorf("Track Progress 引用未知或重复 Track: %s", change.TrackID)
		}
		if change.Progress == "" {
			return fmt.Errorf("Track %s 的进度为空", change.TrackID)
		}
		seen[change.TrackID] = true
	}

	return nil
}

func applyCharacterChanges(current []CharacterState, changes []CharacterStateChange) []CharacterState {
	byID := make(map[string]string, len(changes))
	for _, change := range changes {
		byID[change.CharacterID] = change.State
	}

	for index := range current {
		if state, exists := byID[current[index].CharacterID]; exists {
			current[index].State = state
		}
	}

	return current
}

func applyDurableChanges(current []DurableState, changes []DurableStateChange) []DurableState {
	for _, change := range changes {
		if change.Operation == durableRemove {
			current = removeDurableState(current, change.ID)
			continue
		}

		current = upsertDurableState(current, change)
	}

	return current
}

func upsertDurableState(current []DurableState, change DurableStateChange) []DurableState {
	for index := range current {
		if current[index].ID == change.ID {
			current[index].Description = change.Description
			return current
		}
	}

	return append(current, DurableState{ID: change.ID, Description: change.Description})
}

func removeDurableState(current []DurableState, id string) []DurableState {
	for index := range current {
		if current[index].ID == id {
			return append(current[:index], current[index+1:]...)
		}
	}

	return current
}

func applyTrackChanges(current []TrackProgress, changes []TrackProgressChange) []TrackProgress {
	for _, change := range changes {
		updated := false
		for index := range current {
			if current[index].TrackID == change.TrackID {
				current[index].Progress = change.Progress
				updated = true
				break
			}
		}
		if !updated {
			current = append(current, TrackProgress{TrackID: change.TrackID, Progress: change.Progress})
		}
	}

	return current
}

func cloneState(state State) State {
	state.CharacterStates = clone(state.CharacterStates)
	state.DurableStates = clone(state.DurableStates)
	state.TrackProgress = clone(state.TrackProgress)
	return state
}

func clone[T any](source []T) []T {
	return append([]T(nil), source...)
}
