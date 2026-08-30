package story

import (
	"fmt"
	"reflect"
	"strings"
)

var validThreadStatus = map[string]bool{"active": true, "dormant": true, "resolved": true}
var validProgressStatus = map[string]bool{"ongoing": true, "completed": true, "blocked": true}
var validStoryStatus = map[string]bool{"ongoing": true, "ending": true, "completed": true}
var validReaderAction = map[string]bool{"continue": true, "adjust": true, "replan": true}
var validLengthProfile = map[string]bool{"short": true, "medium": true, "long": true, "epic": true}

// ValidateProject validates operational inputs only. LengthProfile remains a
// creative scale hint and never expands into numeric chapter or word limits.
func ValidateProject(project Project) error {
	if strings.TrimSpace(project.Idea) == "" {
		return fmt.Errorf("小说点子不能为空")
	}
	profile := strings.ToLower(strings.TrimSpace(project.LengthProfile))
	if !validLengthProfile[profile] {
		return fmt.Errorf("篇幅意图必须是 short、medium、long 或 epic")
	}
	if project.MaxCalls < 1 {
		return fmt.Errorf("模型调用上限必须大于 0")
	}
	return nil
}

// ValidateGenesis establishes stable identities and outline references without
// judging how many movements a story ought to contain.
func ValidateGenesis(genesis Genesis) error {
	if strings.TrimSpace(genesis.Bible.Title) == "" {
		return fmt.Errorf("故事标题不能为空")
	}
	if err := validateAudience(genesis.Bible); err != nil {
		return err
	}
	characters, err := collectCharacterIDs(genesis.Bible.Characters)
	if err != nil {
		return err
	}
	if !characters[genesis.Bible.ProtagonistID] {
		return fmt.Errorf("主角 %q 不在人物表中", genesis.Bible.ProtagonistID)
	}
	if err := ValidateOutline(genesis.Outline); err != nil {
		return err
	}
	if genesis.Outline.Version != 0 {
		return fmt.Errorf("初始化大纲版本必须为 0")
	}
	if err := validateInitialCharacters(genesis.InitialState.Characters, characters); err != nil {
		return err
	}
	if err := validateInitialReferences(genesis.InitialState, characters); err != nil {
		return err
	}
	if err := validateInitialThreads(genesis.InitialState.Threads); err != nil {
		return err
	}
	return ValidateReaderState(genesis.InitialReaderState, 0)
}

func validateAudience(bible StoryBible) error {
	reader := bible.TargetReader
	if strings.TrimSpace(reader.Name) == "" || strings.TrimSpace(reader.ReadingHistory) == "" {
		return fmt.Errorf("目标读者必须是有名字和具体阅读经历的人")
	}
	for _, broad := range []string{"目标读者", "大众", "青少年", "青年人", "年轻人", "女性读者", "男性读者", "悬疑爱好者"} {
		if strings.Contains(reader.Name, broad) {
			return fmt.Errorf("目标读者 %q 仍是宽泛人群，必须具体到一个人", reader.Name)
		}
	}
	if len(reader.Craves) == 0 || len(reader.DropsWhen) == 0 || len(reader.BingeTriggers) == 0 {
		return fmt.Errorf("目标读者必须说明渴望、弃读点和追读触发点")
	}
	if strings.TrimSpace(bible.NarrativePromise.PrimaryPleasure) == "" {
		return fmt.Errorf("叙事承诺必须声明首要阅读快感")
	}
	if len(bible.NarrativePromise.MustDeliver) == 0 || len(bible.NarrativePromise.MustNotBecome) == 0 {
		return fmt.Errorf("叙事承诺必须说明必须兑现与不能变成什么")
	}
	return nil
}

func ValidateOutline(outline StoryOutline) error {
	if strings.TrimSpace(outline.CoreConflict) == "" {
		return fmt.Errorf("大纲核心冲突不能为空")
	}
	seen := make(map[string]bool, len(outline.Movements))
	for _, movement := range outline.Movements {
		if movement.ID == "" || seen[movement.ID] {
			return fmt.Errorf("故事阶段 ID 为空或重复: %s", movement.ID)
		}
		seen[movement.ID] = true
	}
	if len(seen) == 0 || !seen[outline.CurrentMovementID] {
		return fmt.Errorf("当前故事阶段不存在: %s", outline.CurrentMovementID)
	}
	return nil
}

// ValidateReplan prevents a replanner from rewriting completed history.
func ValidateReplan(old, next StoryOutline, completed []string) error {
	if err := ValidateOutline(next); err != nil {
		return err
	}
	oldByID, nextByID := movementIndex(old), movementIndex(next)
	for _, id := range completed {
		if _, ok := oldByID[id]; !ok {
			return fmt.Errorf("完成账本引用旧大纲中不存在的阶段: %s", id)
		}
		if _, ok := nextByID[id]; !ok {
			return fmt.Errorf("重规划删除了已完成阶段: %s", id)
		}
		if !reflect.DeepEqual(oldByID[id], nextByID[id]) {
			return fmt.Errorf("重规划改写了已完成阶段: %s", id)
		}
		if id == next.CurrentMovementID {
			return fmt.Errorf("已完成阶段不能再次成为当前阶段: %s", id)
		}
	}
	return nil
}

func movementIndex(outline StoryOutline) map[string]StoryMovement {
	result := make(map[string]StoryMovement, len(outline.Movements))
	for _, movement := range outline.Movements {
		result[movement.ID] = movement
	}
	return result
}

func ValidateReaderState(state ReaderState, chapter int) error {
	if state.Chapter != chapter {
		return fmt.Errorf("读者状态章节应为 %d，实际为 %d", chapter, state.Chapter)
	}
	if !validReaderAction[state.SuggestedAction] {
		return fmt.Errorf("读者建议动作无效: %s", state.SuggestedAction)
	}
	if chapter == 0 && len(state.LosingPatienceWith) > 0 {
		return fmt.Errorf("初始读者状态不能凭空失去耐心")
	}
	return nil
}

func collectCharacterIDs(characters []Character) (map[string]bool, error) {
	ids := make(map[string]bool, len(characters))
	for _, character := range characters {
		if character.ID == "" || character.Name == "" || ids[character.ID] {
			return nil, fmt.Errorf("人物 ID/姓名为空或 ID 重复: %s", character.ID)
		}
		ids[character.ID] = true
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("故事至少需要一个人物")
	}
	return ids, nil
}

func validateInitialCharacters(states []CharacterState, known map[string]bool) error {
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		if !known[state.CharacterID] || seen[state.CharacterID] {
			return fmt.Errorf("初始人物状态未知或重复: %s", state.CharacterID)
		}
		seen[state.CharacterID] = true
	}
	if len(seen) != len(known) {
		return fmt.Errorf("每个故事人物都必须有初始状态")
	}
	return nil
}

func validateInitialThreads(threads []PlotThreadState) error {
	seen := make(map[string]bool, len(threads))
	for _, thread := range threads {
		if thread.ID == "" || thread.Name == "" || seen[thread.ID] || !validThreadStatus[thread.Status] {
			return fmt.Errorf("初始剧情线无效: %s/%s", thread.ID, thread.Status)
		}
		seen[thread.ID] = true
	}
	return nil
}

func validateInitialReferences(initial InitialState, characters map[string]bool) error {
	facts, err := collectFactIDs(initial.Facts)
	if err != nil {
		return err
	}
	for _, character := range initial.Characters {
		if err := validateCharacterReferences(character, characters, facts); err != nil {
			return err
		}
	}
	return nil
}

func collectFactIDs(facts []Fact) (map[string]bool, error) {
	ids := make(map[string]bool, len(facts))
	for _, fact := range facts {
		if fact.ID == "" || ids[fact.ID] {
			return nil, fmt.Errorf("事实 ID 为空或重复: %s", fact.ID)
		}
		ids[fact.ID] = true
	}
	return ids, nil
}

func validateCharacterReferences(state CharacterState, characters, facts map[string]bool) error {
	for _, knowledge := range state.Knowledge {
		if !facts[knowledge.FactID] {
			return fmt.Errorf("人物 %s 引用未知事实: %s", state.CharacterID, knowledge.FactID)
		}
	}
	for _, relationship := range state.Relationships {
		if !characters[relationship.TargetID] {
			return fmt.Errorf("人物 %s 引用未知关系对象: %s", state.CharacterID, relationship.TargetID)
		}
	}
	return nil
}
