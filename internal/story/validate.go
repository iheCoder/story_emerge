package story

import (
	"fmt"
	"strings"
)

// validThreadStatus 是状态机允许的剧情线生命周期；刻意不接受自由文本，
// 这样规划器和审核器可以稳定判断“未解决”与“已回收”。
var validThreadStatus = map[string]bool{
	"active": true, "dormant": true, "resolved": true,
}

// ChapterRangeForLength 把读者可理解的篇幅档位转换为总导演的决策边界。
// 这里给出范围而不是固定章节数，具体结构仍由总导演根据故事本身决定。
func ChapterRangeForLength(profile string) (minimum, maximum int, valid bool) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "short":
		return 6, 8, true
	case "medium":
		return 12, 18, true
	case "long":
		return 24, 30, true
	default:
		return 0, 0, false
	}
}

// ValidateProject 尽早拒绝无法形成稳定写作任务的输入，避免花费模型调用后才报错。
func ValidateProject(project Project) error {
	// 点子、章节范围和调用预算是生成链路的硬前置条件；任何一项无效都不进入模型阶段。

	// 确认创作点子可用。
	if strings.TrimSpace(project.Idea) == "" {
		return fmt.Errorf("小说点子不能为空")
	}
	if project.TargetChapters == 0 {
		if _, _, valid := ChapterRangeForLength(project.LengthProfile); !valid {
			return fmt.Errorf("未指定目标章节数时必须提供有效篇幅档位")
		}
	} else if project.TargetChapters < 3 {
		return fmt.Errorf("目标章节数不能少于 3")
	}

	// 确认章节字数窗口和调用预算足以支撑状态机运行。
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
	// 按标题/人物/阶段/导演提示/初始状态引用/剧情线的顺序检查，
	// 让错误优先落在最早破坏后续上下文的结构上。
	// 检查 Bible 的标题和人物白名单，先建立所有引用的主键。
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

	// 检查故事阶段是否连续覆盖全书。
	if err := validateArcs(genesis.Bible.Arcs, targetChapters); err != nil {
		return err
	}

	// 检查总导演提示数量，控制初始上下文规模。
	if len(genesis.InitialState.DirectorGuidance) < 1 || len(genesis.InitialState.DirectorGuidance) > 5 {
		return fmt.Errorf("总导演初始提示应为 1～5 条，实际为 %d", len(genesis.InitialState.DirectorGuidance))
	}

	// 检查初始人物状态和跨表引用。
	if err := validateInitialCharacters(genesis.InitialState.Characters, characterIDs); err != nil {
		return err
	}
	if err := validateInitialReferences(genesis.InitialState, characterIDs); err != nil {
		return err
	}

	// 检查初始剧情线是否可进入第 1 章。
	return validateInitialThreads(genesis.InitialState.Threads, targetChapters)
}

// validateArcs 校验阶段数量以及从第 1 章到目标章节的连续覆盖。
func validateArcs(arcs []StoryArc, targetChapters int) error {
	// 阶段必须从第 1 章连续覆盖到目标章节；不允许空档，因为规划器会按范围选当前阶段。
	// 限制阶段数量，避免规划器面对过多相互竞争的长期目标。
	if len(arcs) < 2 || len(arcs) > 5 {
		return fmt.Errorf("故事阶段数量应为 2～5，实际为 %d", len(arcs))
	}

	// 逐段验证起止章节连续且不倒置。
	expectedStart := 1
	for _, arc := range arcs {
		if arc.StartChapter != expectedStart || arc.EndChapter < arc.StartChapter {
			return fmt.Errorf("故事阶段 %s 的章节范围不连续", arc.ID)
		}
		expectedStart = arc.EndChapter + 1
	}

	// 确认最后一段恰好覆盖目标章节。
	if expectedStart != targetChapters+1 {
		return fmt.Errorf("故事阶段没有完整覆盖 1～%d 章", targetChapters)
	}
	return nil
}

// collectCharacterIDs 构建并校验静态人物白名单。
func collectCharacterIDs(characters []Character) (map[string]bool, error) {
	// 先建立唯一 ID 索引，后续主角、关系和人物状态都用同一份索引做引用校验。

	// 逐个人物校验必填字段和 ID 唯一性。
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

	// 拒绝空人物表，保证后续所有故事至少有一个叙事主体。
	if len(ids) == 0 {
		return nil, fmt.Errorf("故事至少需要一个人物")
	}
	return ids, nil
}

// validateInitialCharacters 确保每个静态人物恰好有一份初始动态状态。
func validateInitialCharacters(states []CharacterState, known map[string]bool) error {
	// 初始状态必须与静态人物表一一对应：既不能多出临时人物，也不能漏掉正式人物。
	// 逐条检查状态只引用正式人物且不重复。
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

	// 确认每个正式人物都拥有一份初始状态。
	if len(seen) != len(known) {
		return fmt.Errorf("每个故事人物都必须有初始状态")
	}
	return nil
}

// validateInitialThreads 校验初始剧情线的唯一性、状态和计划回收范围。
func validateInitialThreads(threads []PlotThreadState, targetChapters int) error {
	// 初始剧情线的 ID、名称、状态和回收章节都在此锁定，避免第 1 章才发现账本不可用。
	// 建立剧情线 ID 索引并检查基础字段。
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

// validateInitialReferences 校验初始人物知识与关系是否引用已存在对象。
func validateInitialReferences(initial InitialState, characterIDs map[string]bool) error {
	// 事实索引先于人物认知校验建立，确保“人物知道什么”只指向真实事实。
	// 建立初始事实索引。
	factIDs, err := collectFactIDs(initial.Facts)
	if err != nil {
		return err
	}

	// 逐个人物检查知识和关系引用。
	for _, character := range initial.Characters {
		if err := validateCharacterReferences(character, characterIDs, factIDs); err != nil {
			return err
		}
	}
	return nil
}

// collectFactIDs 构建跨章节事实引用索引，并拒绝空/重复主键。
func collectFactIDs(facts []Fact) (map[string]bool, error) {
	// 事实 ID 是跨章节知识引用的主键，因此空值和重复值都必须在初始化阶段拒绝。

	// 遍历事实并建立唯一索引。
	ids := make(map[string]bool, len(facts))
	for _, fact := range facts {
		if fact.ID == "" || ids[fact.ID] {
			return nil, fmt.Errorf("事实 ID 为空或重复: %s", fact.ID)
		}
		ids[fact.ID] = true
	}
	return ids, nil
}

// validateCharacterReferences 校验单个人物状态中的知识和关系白名单引用。
func validateCharacterReferences(state CharacterState, characters, facts map[string]bool) error {
	// Knowledge 和 Relationships 是最容易被模型写成幻觉引用的两个入口，
	// 分别对事实表和人物表做白名单校验。
	// 检查人物认知指向已存在事实。
	for _, knowledge := range state.Knowledge {
		if !facts[knowledge.FactID] {
			return fmt.Errorf("人物 %s 引用未知事实: %s", state.CharacterID, knowledge.FactID)
		}
	}

	// 检查人物关系指向正式人物。
	for _, relationship := range state.Relationships {
		if !characters[relationship.TargetID] {
			return fmt.Errorf("人物 %s 引用未知关系对象: %s", state.CharacterID, relationship.TargetID)
		}
	}
	return nil
}
