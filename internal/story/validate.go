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
	if err := ValidateSpine(genesis.StorySpine); err != nil {
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

	// 承诺数量由创作提示词指导，不因条数偏离建议而拒绝整个作品核心。
	// 已提供的承诺仍须说明内容与兑现方式，供后续生成和完结判断使用。
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
	if !nonempty(direction.CurrentPosition) || !nonempty(direction.Focus) || !nonempty(direction.DesiredShift) || !nonempty(direction.ReaderExpectation) {
		return fmt.Errorf("当前方向缺少当前位置、重心、期望变化或读者期待")
	}
	return nil
}

// ValidateSpine 只检查参照是否完整可读。阶段多少、因果是否成立、变化是否充分都是文学判断，
// 不在代码中比较前后状态文本、强制串行依赖或设置题材规则。
func ValidateSpine(spine []SpineStage) error {
	if len(spine) == 0 {
		return fmt.Errorf("缺少 Story Spine")
	}
	for index, stage := range spine {
		if !nonempty(stage.From) || !nonempty(stage.To) || !nonempty(stage.WhyItMatters) || !nonempty(stage.ExitEvidence) {
			return fmt.Errorf("Story Spine 第 %d 项缺少变化、因果作用或成立依据", index+1)
		}
	}
	return nil
}

// ValidateDirectorDecision 校验 Story Director 的阶段级输出。
func ValidateDirectorDecision(current Direction, decision DirectorDecision) error {
	if err := ValidateDirection(decision.Direction); err != nil {
		return err
	}
	if !nonempty(decision.Reason) {
		return fmt.Errorf("Story Director 缺少判断依据")
	}

	// KEEP 的价值是给方向提供跨章惯性，不能借 KEEP 偷偷润色或改写任何字段。
	// ADJUST/REPLACE 则必须真的产生变化，避免动作名称与实际结果相互矛盾。
	switch decision.Action {
	case DirectorKeep:
		if decision.Direction != current {
			return fmt.Errorf("Story Director KEEP 必须原样返回当前方向")
		}
	case DirectorAdjust, DirectorReplace:
		if decision.Direction == current {
			return fmt.Errorf("Story Director %s 没有改变当前方向", decision.Action)
		}
	default:
		return fmt.Errorf("Story Director action 无效: %s", decision.Action)
	}
	return nil
}
func ValidateEditorDecision(decision EditorDecision) error {
	if !nonempty(decision.Assessment.Contribution) || !nonempty(decision.Assessment.Sequence) || !nonempty(decision.Assessment.Execution) {
		return fmt.Errorf("Editor 缺少章节贡献、序列效果或正文实现判断")
	}

	// 问题数量不决定评审是否有效；保留全部问题供修订，但动作与问题必须一致。
	switch decision.ChapterDecision {
	case EditorAccept:
		if len(decision.BlockingIssues) != 0 {
			return fmt.Errorf("ACCEPT 不能带有未解决的阻断问题")
		}
	case EditorRevise:
		if decision.StoryComplete {
			return fmt.Errorf("未接受的正文不能确认完结")
		}
		if len(decision.BlockingIssues) == 0 {
			return fmt.Errorf("退回必须说明需要修复或重新考虑的问题")
		}
	default:
		return fmt.Errorf("Editor chapter_decision 无效: %s", decision.ChapterDecision)
	}
	for _, issue := range decision.BlockingIssues {
		if !nonempty(issue) {
			return fmt.Errorf("阻断问题不能为空")
		}
	}
	if decision.DirectionReview.Requested != nonempty(decision.DirectionReview.Reason) {
		return fmt.Errorf("Editor 的 Direction Review 请求与原因不一致")
	}
	if decision.ChapterDecision != EditorAccept && decision.DirectionReview.Requested {
		return fmt.Errorf("未接受的正文不能请求 Direction Review")
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
		// 人物内部状态现在可以逐条更新，因此每条都必须拥有非空且字段内唯一的 ID。
		// Value 仍是给创作角色阅读的内容；ID 只作为 reducer 的定位键，不能容忍空值或歧义覆盖。
		if err := validateStateItems(character.ID, "facts", character.Facts); err != nil {
			return err
		}
		if err := validateStateItems(character.ID, "knowledge_and_beliefs", character.KnowledgeAndBeliefs); err != nil {
			return err
		}
		if err := validateStateItems(character.ID, "commitments_and_intentions", character.CommitmentsAndIntentions); err != nil {
			return err
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

func validateStateItems(characterID, field string, items []StateItem) error {
	ids := map[string]bool{}
	for _, item := range items {
		if !nonempty(item.ID) || ids[item.ID] || !nonempty(item.Value) {
			return fmt.Errorf("人物 %s 的 %s 条目 ID 或内容无效: %s", characterID, field, item.ID)
		}
		ids[item.ID] = true
	}
	return nil
}
func ValidateState(state State) error {
	if state.Chapter < 0 || state.WrittenCharacters < 0 || state.DirectionVersion < 1 {
		return fmt.Errorf("检查点章节或字数无效")
	}
	if state.DirectionReviewedAfterChapter < 0 || state.DirectionReviewedAfterChapter > state.Chapter {
		return fmt.Errorf("Direction 复查章节无效")
	}
	if state.Chapter == 0 && (state.Completed || state.WrittenCharacters != 0 || state.DirectionReviewedAfterChapter != 0) {
		return fmt.Errorf("初始检查点不能已经完结或包含正文字数")
	}
	if err := ValidateDirection(state.Direction); err != nil {
		return err
	}
	if err := ValidateSpine(state.StorySpine); err != nil {
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
