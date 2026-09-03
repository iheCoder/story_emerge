package story

import (
	"fmt"
	"strings"
)

func nonempty(value string) bool { return strings.TrimSpace(value) != "" }
func ValidateProject(project Project) error {
	if !nonempty(project.Idea) {
		return fmt.Errorf("创作点子不能为空")
	}
	if LengthGoal(project.LengthProfile) == "" {
		return fmt.Errorf("故事规模无效: %s", project.LengthProfile)
	}
	if project.MaxCalls < 1 {
		return fmt.Errorf("模型调用预算必须大于 0")
	}
	return nil
}
func ValidateGenesis(genesis Genesis) error {
	if !nonempty(genesis.Title) {
		return fmt.Errorf("缺少书名")
	}
	if err := ValidateCore(genesis.StoryCore); err != nil {
		return err
	}
	if err := ValidateDirection(genesis.CurrentDirection); err != nil {
		return err
	}
	return ValidateStoryState(genesis.InitialStoryState)
}
func ValidateCore(core StoryCore) error {
	if !nonempty(core.StoryEngine.Loop) || !nonempty(core.StoryEngine.ProgressionAxis) {
		return fmt.Errorf("Story Engine 缺少循环或累积方向")
	}
	if len(core.ReaderPromises) < 3 || len(core.ReaderPromises) > 5 {
		return fmt.Errorf("Reader Promises 应保留 3～5 个核心承诺")
	}
	for _, promise := range core.ReaderPromises {
		if !nonempty(promise.Promise) || !nonempty(promise.PayoffShape) {
			return fmt.Errorf("核心承诺缺少内容或兑现方式")
		}
	}
	if !nonempty(core.ExperienceContract.TargetExperience) {
		return fmt.Errorf("缺少目标阅读体验")
	}
	return nil
}
func ValidateDirection(direction Direction) error {
	if !nonempty(direction.Focus) || !nonempty(direction.DesiredShift) {
		return fmt.Errorf("当前方向缺少重心或期望变化")
	}
	return nil
}
func ValidatePlan(plan ChapterPlan) error {
	switch plan.DirectionAction {
	case "KEEP":
		if plan.CurrentDirection != nil {
			return fmt.Errorf("KEEP 不能同时修改方向")
		}
	case "UPDATE":
		if plan.CurrentDirection == nil {
			return fmt.Errorf("UPDATE 缺少新方向")
		}
		if err := ValidateDirection(*plan.CurrentDirection); err != nil {
			return err
		}
	default:
		return fmt.Errorf("无效方向操作: %s", plan.DirectionAction)
	}
	if !nonempty(plan.ChapterIntent.IntendedEffect) || !nonempty(plan.ChapterIntent.WhyNow) {
		return fmt.Errorf("章节意图缺少叙事效果或时机理由")
	}
	if len(plan.ChapterIntent.Constraints) > 2 {
		return fmt.Errorf("章节约束最多两条")
	}
	return nil
}
func ValidateEditorDecision(decision EditorDecision) error {
	if !nonempty(decision.Reason) {
		return fmt.Errorf("Editor 缺少判断原因")
	}
	if len(decision.BlockingIssues) > 3 {
		return fmt.Errorf("Editor 最多返回三个阻断问题")
	}
	switch decision.Action {
	case EditorAccept:
		if len(decision.BlockingIssues) != 0 {
			return fmt.Errorf("ACCEPT 不能带有未解决的阻断问题")
		}
	case EditorRevise, EditorReplan:
		if decision.StoryComplete {
			return fmt.Errorf("未接受的正文不能确认完结")
		}
		if len(decision.BlockingIssues) == 0 {
			return fmt.Errorf("退回必须说明需要修复或重新考虑的问题")
		}
	default:
		return fmt.Errorf("Editor action 无效: %s", decision.Action)
	}
	for _, issue := range decision.BlockingIssues {
		if !nonempty(issue) {
			return fmt.Errorf("阻断问题不能为空")
		}
	}
	return nil
}
func ValidateCommitResult(result CommitResult) error {
	if !nonempty(result.ChapterSummary) || !nonempty(result.TrajectoryEntry.StoryMove) || !nonempty(result.TrajectoryEntry.NarrativeShape) {
		return fmt.Errorf("Commit 缺少章节摘要或真实轨迹")
	}
	return nil
}

// ValidateStoryState 只保护 ID 和引用。是否把猜测写成事实由提取提示词负责，
// 程序不能通过关键词、人物数量或题材类别猜测文学语义。
func ValidateStoryState(state CurrentStoryState) error {
	world := map[string]bool{}
	for _, fact := range state.World {
		if !nonempty(fact.ID) || world[fact.ID] || !nonempty(fact.Description) {
			return fmt.Errorf("世界事实 ID 或内容无效: %s", fact.ID)
		}
		world[fact.ID] = true
	}
	characters := map[string]bool{}
	for _, character := range state.Characters {
		if !nonempty(character.ID) || characters[character.ID] || !nonempty(character.Name) {
			return fmt.Errorf("人物 ID 或姓名无效: %s", character.ID)
		}
		characters[character.ID] = true
	}
	relations := map[string]bool{}
	for _, relation := range state.Relationships {
		if !nonempty(relation.ID) || relations[relation.ID] || !nonempty(relation.Description) {
			return fmt.Errorf("关系无效: %s", relation.ID)
		}

		relations[relation.ID] = true

		// 参与者索引不要求覆盖关系中所有人，避免为尚未建档的配角阻断章节提交。
		// 已提供的引用仍必须存在且不重复；关系是否值得保存由提取提示词指导。
		participants := map[string]bool{}
		for _, id := range relation.Characters {
			if !characters[id] || participants[id] {
				return fmt.Errorf("关系 %s 引用未知或重复人物: %s", relation.ID, id)
			}
			participants[id] = true
		}
	}
	return nil
}
func ValidateState(state State) error {
	if state.Chapter < 0 || state.WrittenCharacters < 0 {
		return fmt.Errorf("检查点章节或字数无效")
	}
	if state.Chapter == 0 && (state.Completed || state.WrittenCharacters != 0) {
		return fmt.Errorf("初始检查点不能已经完结或包含正文字数")
	}
	if err := ValidateDirection(state.Direction); err != nil {
		return err
	}
	if len(state.RecentTrajectory) != min(state.Chapter, RecentTrajectoryLimit) {
		return fmt.Errorf("近期轨迹与检查点章节不一致")
	}
	for i, entry := range state.RecentTrajectory {
		if entry.Chapter != state.Chapter-len(state.RecentTrajectory)+i+1 || !nonempty(entry.StoryMove) || !nonempty(entry.NarrativeShape) {
			return fmt.Errorf("近期轨迹缺失或顺序无效")
		}
	}
	return ValidateStoryState(state.Story)
}
