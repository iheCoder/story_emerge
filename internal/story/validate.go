package story

import (
	"fmt"
	"strings"
)

var validStoryStatus = map[string]bool{"ongoing": true, "ending": true, "completed": true}
var validTrackStatus = map[string]bool{"ongoing": true, "completed": true}

func ValidateProject(project Project) error {
	if strings.TrimSpace(project.Idea) == "" {
		return fmt.Errorf("创作点子不能为空")
	}
	if !map[string]bool{"short": true, "medium": true, "long": true, "epic": true}[project.LengthProfile] {
		return fmt.Errorf("故事规模无效: %s", project.LengthProfile)
	}
	if project.MaxCalls < 1 {
		return fmt.Errorf("模型调用预算必须大于 0")
	}

	return nil
}

// ValidateGenesis 检查 Architect 输出的稳定结构与跨对象引用。
func ValidateGenesis(genesis Genesis) error {
	if err := ValidateBible(genesis.Bible); err != nil {
		return err
	}
	if err := ValidateOutline(genesis.Outline); err != nil {
		return err
	}
	if genesis.Outline.Version != 0 {
		return fmt.Errorf("初始大纲版本必须为 0")
	}

	return validateInitialState(genesis)
}

func ValidateBible(bible StoryBible) error {
	if strings.TrimSpace(bible.Title) == "" || strings.TrimSpace(bible.Premise) == "" {
		return fmt.Errorf("Story Bible 缺少书名或 Premise")
	}
	if strings.TrimSpace(bible.StorySpine) == "" || strings.TrimSpace(bible.EndingDirection) == "" {
		return fmt.Errorf("Story Bible 缺少 Story Spine 或结局方向")
	}
	if err := validateAudience(bible); err != nil {
		return err
	}

	seen := make(map[string]bool, len(bible.Characters))
	for _, character := range bible.Characters {
		if character.ID == "" || character.Name == "" || seen[character.ID] {
			return fmt.Errorf("人物 ID 为空、重复或缺少姓名: %s", character.ID)
		}
		seen[character.ID] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("Story Bible 至少需要一个人物")
	}

	return nil
}

func validateAudience(bible StoryBible) error {
	reader := bible.TargetReader
	if strings.TrimSpace(reader.Portrait) == "" || strings.TrimSpace(reader.ReadsFor) == "" || strings.TrimSpace(reader.LeavesWhen) == "" {
		return fmt.Errorf("目标读者必须具有具体画像、阅读动机和弃读边界")
	}
	promise := bible.NarrativePromise
	if strings.TrimSpace(promise.CoreExperience) == "" || len(promise.MustRemain) == 0 || len(promise.MustNotBecome) == 0 {
		return fmt.Errorf("叙事承诺缺少核心体验、必须保持或禁止滑向")
	}

	return nil
}

func ValidateOutline(outline StoryOutline) error {
	if outline.CurrentArc.Name == "" || outline.CurrentArc.Purpose == "" {
		return fmt.Errorf("Current Arc 缺少名称或目的")
	}

	seen := make(map[string]bool, len(outline.Tracks))
	for _, track := range outline.Tracks {
		if track.ID == "" || track.Name == "" || seen[track.ID] {
			return fmt.Errorf("Story Track ID 为空、重复或缺少名称: %s", track.ID)
		}
		if track.Direction == "" || !validTrackStatus[track.Status] {
			return fmt.Errorf("Story Track 字段无效: %s", track.ID)
		}
		seen[track.ID] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("大纲至少需要一条 Story Track")
	}

	return nil
}

// ValidateReplan 只验证新的未来大纲自身合法且版本递增。
// Bible 与已提交正文根本不在 Architect replan 的输出结构中，因此无法被修改。
func ValidateReplan(old, next StoryOutline) error {
	if err := ValidateOutline(next); err != nil {
		return err
	}
	if next.Version != old.Version+1 {
		return fmt.Errorf("重规划版本应为 %d，实际为 %d", old.Version+1, next.Version)
	}

	return nil
}

func validateInitialState(genesis Genesis) error {
	characters := make(map[string]bool, len(genesis.Bible.Characters))
	for _, character := range genesis.Bible.Characters {
		characters[character.ID] = true
	}

	if err := validateInitialCharacters(genesis.InitialState.CharacterStates, characters); err != nil {
		return err
	}

	return validateInitialSituationStates(genesis.InitialState.SituationStates)
}

func validateInitialCharacters(states []CharacterState, known map[string]bool) error {
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		if !known[state.CharacterID] || seen[state.CharacterID] || state.State == "" {
			return fmt.Errorf("初始人物状态无效: %s", state.CharacterID)
		}
		seen[state.CharacterID] = true
	}
	if len(seen) != len(known) {
		return fmt.Errorf("每个 Bible 人物都必须有初始长期状态")
	}

	return nil
}

func validateInitialSituationStates(states []SituationState) error {
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		if state.ID == "" || state.Description == "" || seen[state.ID] {
			return fmt.Errorf("初始局势状态无效: %s", state.ID)
		}
		seen[state.ID] = true
	}

	return nil
}

func ValidateEditorDecision(decision EditorDecision, chapter int) error {
	if decision.Action != EditorAccept && decision.Action != EditorRevise && decision.Action != EditorReplan {
		return fmt.Errorf("Editor action 无效: %s", decision.Action)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return fmt.Errorf("Editor decision 缺少原因")
	}

	switch decision.Action {
	case EditorAccept:
		return validateFinalizedEditorial(decision.StoryUpdate, decision.Summary, chapter)
	case EditorRevise:
		if strings.TrimSpace(decision.Guidance) == "" {
			return fmt.Errorf("revise 缺少 Guidance")
		}
	case EditorReplan:
		if strings.TrimSpace(decision.Guidance) == "" {
			return fmt.Errorf("replan 缺少 Guidance")
		}
	}

	return nil
}

func ValidateEditorFinalize(result EditorFinalizeResult, chapter int) error {
	return validateFinalizedEditorial(result.StoryUpdate, result.Summary, chapter)
}

func validateFinalizedEditorial(update StoryUpdate, summary ChapterSummary, chapter int) error {
	if update.Chapter != chapter || summary.Number != chapter {
		return fmt.Errorf("Editor 最终产物章节不一致: %d/%d/%d", update.Chapter, summary.Number, chapter)
	}
	if summary.Title == "" || summary.Summary == "" {
		return fmt.Errorf("读者可见摘要缺少标题或内容")
	}

	return nil
}

func ValidateReaderObservation(observation ReaderObservation, chapter int) error {
	if observation.Chapter != chapter {
		return fmt.Errorf("Reader Observation 章节应为 %d，实际为 %d", chapter, observation.Chapter)
	}

	return nil
}
