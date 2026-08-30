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

// ValidateProject 只验证启动工作流所需的操作参数。
// LengthProfile 是创作规模意图，不能在这里转换成章节数或字数配额。
func ValidateProject(project Project) error {
	// 创作点子是 Architect 唯一的用户事实来源，空输入无法建立故事。
	if strings.TrimSpace(project.Idea) == "" {
		return fmt.Errorf("小说点子不能为空")
	}

	// 规模档位只控制全书量级语义；枚举用于拒绝拼写错误，不携带文学规则。
	profile := strings.ToLower(strings.TrimSpace(project.LengthProfile))
	if !validLengthProfile[profile] {
		return fmt.Errorf("篇幅意图必须是 short、medium、long 或 epic")
	}

	// 调用预算是成本护栏，必须在任何模型请求前确定。
	if project.MaxCalls < 1 {
		return fmt.Errorf("模型调用上限必须大于 0")
	}

	return nil
}

// ValidateGenesis 建立后续章节依赖的稳定主键和引用关系。
// 它不判断题材、情节好坏、movement 数量或目标读者选择是否聪明。
func ValidateGenesis(genesis Genesis) error {
	// 阶段一：验证 Bible 的最小创作契约。
	if strings.TrimSpace(genesis.Bible.Title) == "" {
		return fmt.Errorf("故事标题不能为空")
	}
	if err := validateAudience(genesis.Bible); err != nil {
		return err
	}

	// 阶段二：建立正式人物白名单，并确认主角引用有效。
	characters, err := collectCharacterIDs(genesis.Bible.Characters)
	if err != nil {
		return err
	}
	if !characters[genesis.Bible.ProtagonistID] {
		return fmt.Errorf("主角 %q 不在人物表中", genesis.Bible.ProtagonistID)
	}

	// 阶段三：验证初始大纲是可寻址的第 0 版。
	if err := ValidateOutline(genesis.Outline); err != nil {
		return err
	}
	if genesis.Outline.Version != 0 {
		return fmt.Errorf("初始化大纲版本必须为 0")
	}

	// 阶段四：按人物、跨表引用、剧情线生命周期的依赖顺序验证初态。
	if err := validateInitialCharacters(genesis.InitialState.Characters, characters); err != nil {
		return err
	}
	if err := validateInitialReferences(genesis.InitialState, characters); err != nil {
		return err
	}
	if err := validateInitialThreads(genesis.InitialState.Threads); err != nil {
		return err
	}

	// 阶段五：Reader 初态也属于 Architect 的第 0 章交付，必须对应同一章节。
	return ValidateReaderState(genesis.InitialReaderState, 0)
}

// validateAudience 只检查画像是否具备可供 Writer 使用的字段。
// “这个人是否真的具体、是否选对”属于 Architect 的语义判断，程序不再用题材词表猜测。
func validateAudience(bible StoryBible) error {
	reader := bible.TargetReader

	// 姓名和阅读经历共同标识一个可想象的人；程序不检查名字里出现什么词。
	if strings.TrimSpace(reader.Name) == "" || strings.TrimSpace(reader.ReadingHistory) == "" {
		return fmt.Errorf("目标读者必须是有名字和具体阅读经历的人")
	}

	// 三类偏好是 Writer 进行取舍所需的最小信息，而不是评分配额。
	if len(reader.Craves) == 0 || len(reader.DropsWhen) == 0 || len(reader.BingeTriggers) == 0 {
		return fmt.Errorf("目标读者必须说明渴望、弃读点和追读触发点")
	}

	// Narrative Promise 是创作与阅读观察共享的体验契约。
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
