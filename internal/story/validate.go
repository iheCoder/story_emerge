package story

import (
	"fmt"
	"strings"
)

var validThreadStatus = map[string]bool{
	"active": true, "dormant": true, "resolved": true,
}

// ValidateProject 尽早拒绝无法形成稳定写作任务的输入，避免花费模型调用后才报错。
func ValidateProject(project Project) error {
	if strings.TrimSpace(project.Idea) == "" {
		return fmt.Errorf("小说点子不能为空")
	}
	if project.TargetChapters < 3 {
		return fmt.Errorf("目标章节数不能少于 3")
	}
	if project.ChapterMinChars < 500 || project.ChapterMaxChars < project.ChapterMinChars {
		return fmt.Errorf("章节字数范围无效")
	}
	if project.MaxCalls < 1 {
		return fmt.Errorf("模型调用上限必须大于 0")
	}
	return nil
}

// ValidateGenesis 检查总导演交付的故事圣经能否作为后续状态机的可靠起点。
func ValidateGenesis(genesis Genesis, targetChapters int) error {
	if strings.TrimSpace(genesis.Bible.Title) == "" {
		return fmt.Errorf("故事标题不能为空")
	}
	characterIDs, err := collectCharacterIDs(genesis.Bible.Characters)
	if err != nil {
		return err
	}
	if !characterIDs[genesis.Bible.ProtagonistID] {
		return fmt.Errorf("主角 %q 不在人物表中", genesis.Bible.ProtagonistID)
	}
	if err := validateArcs(genesis.Bible.Arcs, targetChapters); err != nil {
		return err
	}
	if len(genesis.InitialState.DirectorGuidance) < 1 || len(genesis.InitialState.DirectorGuidance) > 5 {
		return fmt.Errorf("总导演初始提示应为 1～5 条，实际为 %d", len(genesis.InitialState.DirectorGuidance))
	}
	if err := validateInitialCharacters(genesis.InitialState.Characters, characterIDs); err != nil {
		return err
	}
	if err := validateInitialReferences(genesis.InitialState, characterIDs); err != nil {
		return err
	}
	return validateInitialThreads(genesis.InitialState.Threads, targetChapters)
}

func validateArcs(arcs []StoryArc, targetChapters int) error {
	if len(arcs) < 2 || len(arcs) > 5 {
		return fmt.Errorf("故事阶段数量应为 2～5，实际为 %d", len(arcs))
	}
	expectedStart := 1
	for _, arc := range arcs {
		if arc.StartChapter != expectedStart || arc.EndChapter < arc.StartChapter {
			return fmt.Errorf("故事阶段 %s 的章节范围不连续", arc.ID)
		}
		expectedStart = arc.EndChapter + 1
	}
	if expectedStart != targetChapters+1 {
		return fmt.Errorf("故事阶段没有完整覆盖 1～%d 章", targetChapters)
	}
	return nil
}

func collectCharacterIDs(characters []Character) (map[string]bool, error) {
	ids := make(map[string]bool, len(characters))
	for _, character := range characters {
		if character.ID == "" || character.Name == "" {
			return nil, fmt.Errorf("人物 ID 和姓名不能为空")
		}
		if ids[character.ID] {
			return nil, fmt.Errorf("人物 ID 重复: %s", character.ID)
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
		if !known[state.CharacterID] {
			return fmt.Errorf("初始状态引用未知人物: %s", state.CharacterID)
		}
		if seen[state.CharacterID] {
			return fmt.Errorf("人物初始状态重复: %s", state.CharacterID)
		}
		seen[state.CharacterID] = true
	}
	if len(seen) != len(known) {
		return fmt.Errorf("每个故事人物都必须有初始状态")
	}
	return nil
}

func validateInitialThreads(threads []PlotThreadState, targetChapters int) error {
	seen := make(map[string]bool, len(threads))
	for _, thread := range threads {
		if thread.ID == "" || thread.Name == "" {
			return fmt.Errorf("剧情线 ID 和名称不能为空")
		}
		if seen[thread.ID] {
			return fmt.Errorf("剧情线 ID 重复: %s", thread.ID)
		}
		if !validThreadStatus[thread.Status] {
			return fmt.Errorf("剧情线 %s 的状态无效: %s", thread.ID, thread.Status)
		}
		if thread.PlannedPayoffChapter > targetChapters {
			return fmt.Errorf("剧情线 %s 的回收章节超过全书范围", thread.ID)
		}
		seen[thread.ID] = true
	}
	return nil
}

func validateInitialReferences(initial InitialState, characterIDs map[string]bool) error {
	factIDs, err := collectFactIDs(initial.Facts)
	if err != nil {
		return err
	}
	for _, character := range initial.Characters {
		if err := validateCharacterReferences(character, characterIDs, factIDs); err != nil {
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
