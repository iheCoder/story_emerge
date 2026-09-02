package story

import (
	"fmt"
	"strings"
	"unicode"
)

const RecentTrajectoryLimit = 5

func NewInitialState(genesis Genesis) State {
	return State{Story: cloneStoryState(genesis.InitialStoryState), Direction: genesis.CurrentDirection, RecentTrajectory: []TrajectoryEntry{}}
}

// ApplyPatch 在副本上替换当前值。所有集合更新后才校验关系引用，因此同一补丁可以
// 新增人物并建立关系，也可以同时移除人物与失效关系；失败不触碰旧状态。
func ApplyPatch(current CurrentStoryState, patch StatePatch) (CurrentStoryState, error) {
	next := cloneStoryState(current)
	var err error
	next.World, err = applyCollection(next.World, patch.World, func(v WorldFact) string { return v.ID })
	if err != nil {
		return CurrentStoryState{}, err
	}
	next.Characters, err = applyCollection(next.Characters, patch.Characters, func(v CharacterState) string { return v.ID })
	if err != nil {
		return CurrentStoryState{}, err
	}
	next.Relationships, err = applyCollection(next.Relationships, patch.Relationships, func(v RelationshipState) string { return v.ID })
	if err != nil {
		return CurrentStoryState{}, err
	}
	if err := ValidateStoryState(next); err != nil {
		return CurrentStoryState{}, err
	}
	return cloneStoryState(next), nil
}

// applyCollection 统一三类事实集合的 ID 更新规则，不引入自动过期或历史追加策略。
func applyCollection[T any](current []T, patch CollectionPatch[T], id func(T) string) ([]T, error) {
	known, touched := map[string]bool{}, map[string]bool{}
	for _, entry := range current {
		known[id(entry)] = true
	}
	for _, key := range patch.Remove {
		if !known[key] || touched[key] {
			return nil, fmt.Errorf("删除了未知或重复状态: %s", key)
		}
		touched[key] = true
	}
	for _, entry := range patch.Upsert {
		key := id(entry)
		if !nonempty(key) || touched[key] {
			return nil, fmt.Errorf("状态 ID 为空、重复或同时更新删除: %s", key)
		}
		touched[key] = true
	}

	removed := map[string]bool{}
	for _, key := range patch.Remove {
		removed[key] = true
	}
	result := make([]T, 0, len(current)+len(patch.Upsert))
	positions := map[string]int{}
	for _, entry := range current {
		if !removed[id(entry)] {
			positions[id(entry)] = len(result)
			result = append(result, entry)
		}
	}
	for _, entry := range patch.Upsert {
		if index, exists := positions[id(entry)]; exists {
			result[index] = entry
		} else {
			result = append(result, entry)
		}
	}
	return result, nil
}

// ApplyChapter 是正式状态唯一的推进入口。先核验 ACCEPT，再应用提取结果；
// Planner 只决定 Direction，Editor 只确认完结，Commit 无权覆盖这两项。
func ApplyChapter(current State, chapter string, commit ChapterCommit) (State, error) {
	if current.Completed || commit.Chapter != current.Chapter+1 {
		return State{}, fmt.Errorf("不能向已完成故事或错误章节提交")
	}
	if err := ValidatePlan(commit.Plan); err != nil {
		return State{}, err
	}
	if err := ValidateEditorDecision(commit.Review); err != nil {
		return State{}, err
	}
	if commit.Review.Action != EditorAccept {
		return State{}, fmt.Errorf("只有 ACCEPT 正文可以提交")
	}
	if err := ValidateCommitResult(commit.Result); err != nil {
		return State{}, err
	}
	if !nonempty(commit.Title) || CountCharacters(chapter) == 0 {
		return State{}, fmt.Errorf("章节标题或正文为空")
	}
	nextStory, err := ApplyPatch(current.Story, commit.Result.StatePatch)
	if err != nil {
		return State{}, err
	}

	next := current
	next.Story = nextStory
	next.Chapter = commit.Chapter
	if commit.Plan.CurrentDirection != nil {
		next.Direction = *commit.Plan.CurrentDirection
	}
	next.Completed = commit.Review.StoryComplete
	next.WrittenCharacters += CountCharacters(chapter)
	next.RecentTrajectory = append(append([]TrajectoryEntry{}, current.RecentTrajectory...), TrajectoryEntry{Chapter: commit.Chapter, TrajectoryMove: commit.Result.TrajectoryEntry})
	if len(next.RecentTrajectory) > RecentTrajectoryLimit {
		next.RecentTrajectory = next.RecentTrajectory[len(next.RecentTrajectory)-RecentTrajectoryLimit:]
	}
	return next, ValidateState(next)
}

// CountCharacters 统计正文汉字、字母和数字，不计标题、空白和标点。
// 这是全书篇幅信号，绝不能用来自动验收章节或触发完结。
func CountCharacters(chapter string) int {
	_, body, found := strings.Cut(strings.TrimSpace(chapter), "\n")
	if !found {
		return 0
	}
	count := 0
	for _, r := range body {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			count++
		}
	}
	return count
}
func cloneStoryState(state CurrentStoryState) CurrentStoryState {
	state.World = append([]WorldFact{}, state.World...)
	state.Characters = append([]CharacterState{}, state.Characters...)
	for i := range state.Characters {
		c := &state.Characters[i]
		c.Facts = append([]string{}, c.Facts...)
		c.KnowledgeAndBeliefs = append([]string{}, c.KnowledgeAndBeliefs...)
		c.CommitmentsAndIntentions = append([]string{}, c.CommitmentsAndIntentions...)
	}
	state.Relationships = append([]RelationshipState{}, state.Relationships...)
	for i := range state.Relationships {
		state.Relationships[i].Characters = append([]string{}, state.Relationships[i].Characters...)
	}
	return state
}
