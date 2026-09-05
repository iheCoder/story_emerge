package story

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
)

const RecentTrajectoryLimit = 5

func NewInitialState(genesis Genesis) State {
	return State{
		Story: cloneStoryState(genesis.InitialStoryState), Direction: genesis.CurrentDirection,
		StorySpine:       append([]SpineStage{}, genesis.StorySpine...),
		DirectionVersion: 1, RecentTrajectory: []TrajectoryEntry{},
	}
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

	// 人物身份仍按人物 ID 更新，但三类内部状态分别应用 Patch。
	// 因此 Commit 新增一条认知时不再需要重写整个人物，也不会误删本章未提及的旧约束。
	patch, err = ResolveStatePatchIDs(current, patch)
	if err != nil {
		return CurrentStoryState{}, err
	}
	next.Characters, err = applyCharacterPatch(next.Characters, patch.Characters)
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

// ResolveInitialStateItemIDs 给初始世界事实和人物内部条目补齐可重放 ID。
// 人物和关系自身的 ID 涉及相互引用，仍由 Architect 提供，不能猜测补齐。
func ResolveInitialStateItemIDs(state CurrentStoryState) (CurrentStoryState, error) {
	next := cloneStoryState(state)

	// 世界事实没有初态内的身份引用，空 ID 可按内容确定性生成；已有 ID 保持原样。
	// 仅在初始化分配，后续描述变化仍按正式旧 ID 更新，不重新散列或合并事实。
	for index := range next.World {
		fact := &next.World[index]
		if !nonempty(fact.ID) {
			fact.ID = newStateItemID("world", "fact", fact.Description)
		}
	}

	// 人物内部条目沿用原规则；末尾完整校验仍拒绝空内容、重复 ID 和非法引用。
	for index := range next.Characters {
		character := &next.Characters[index]
		var err error
		character.Facts, err = resolveInitialItems(character.ID, "fact", character.Facts)
		if err != nil {
			return CurrentStoryState{}, err
		}
		character.KnowledgeAndBeliefs, err = resolveInitialItems(character.ID, "belief", character.KnowledgeAndBeliefs)
		if err != nil {
			return CurrentStoryState{}, err
		}
		character.CommitmentsAndIntentions, err = resolveInitialItems(character.ID, "commitment", character.CommitmentsAndIntentions)
		if err != nil {
			return CurrentStoryState{}, err
		}
	}
	return next, ValidateStoryState(next)
}

func resolveInitialItems(characterID, field string, items []StateItem) ([]StateItem, error) {
	resolved := append([]StateItem{}, items...)
	seen := map[string]bool{}
	for index := range resolved {
		item := &resolved[index]
		if !nonempty(item.Value) {
			return nil, fmt.Errorf("人物 %s 的 %s 条目内容为空", characterID, field)
		}
		expectedID := newStateItemID(characterID, field, item.Value)
		if !nonempty(item.ID) {
			item.ID = expectedID
		} else if item.ID != expectedID {
			return nil, fmt.Errorf("人物 %s 的 %s 初始条目 ID 必须由程序生成: %s", characterID, field, item.ID)
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("人物 %s 的 %s 条目 ID 重复: %s", characterID, field, item.ID)
		}
		seen[item.ID] = true
	}
	return resolved, nil
}

// ResolveStatePatchIDs 在补丁进入正式 commit 前解析所有新条目 ID。
// 解析是确定性的：同一旧状态和同一模型输出重放会得到相同补丁，commit.json 能完整重建 checkpoint。
func ResolveStatePatchIDs(current CurrentStoryState, patch StatePatch) (StatePatch, error) {
	resolved := cloneStatePatch(patch)

	// 用正式旧状态建立人物索引。新人物没有旧条目，因此其全部内部 upsert 都会走新增规则。
	knownCharacters := map[string]CharacterState{}
	for _, character := range current.Characters {
		knownCharacters[character.ID] = character
	}
	for index := range resolved.Characters.Upsert {
		change := &resolved.Characters.Upsert[index]
		old := knownCharacters[change.ID]
		var err error
		change.Facts, err = resolvePatchItems(change.ID, "fact", old.Facts, change.Facts)
		if err != nil {
			return StatePatch{}, err
		}
		change.KnowledgeAndBeliefs, err = resolvePatchItems(change.ID, "belief", old.KnowledgeAndBeliefs, change.KnowledgeAndBeliefs)
		if err != nil {
			return StatePatch{}, err
		}
		change.CommitmentsAndIntentions, err = resolvePatchItems(change.ID, "commitment", old.CommitmentsAndIntentions, change.CommitmentsAndIntentions)
		if err != nil {
			return StatePatch{}, err
		}
	}
	return resolved, nil
}

func resolvePatchItems(characterID, field string, current []StateItem, patch CollectionPatch[StateItem]) (CollectionPatch[StateItem], error) {
	resolved := CollectionPatch[StateItem]{Upsert: append([]StateItem{}, patch.Upsert...), Remove: append([]string{}, patch.Remove...)}

	// 已有 ID 是本次修改可以引用的白名单。模型不能借一个陌生 ID 绕过“新增条目 ID 留空”的约定。
	known := map[string]bool{}
	for _, item := range current {
		known[item.ID] = true
	}
	for index := range resolved.Upsert {
		item := &resolved.Upsert[index]
		if !nonempty(item.Value) {
			return CollectionPatch[StateItem]{}, fmt.Errorf("人物 %s 的 %s 更新内容为空", characterID, field)
		}
		if !nonempty(item.ID) {
			// 空 ID 明确表示新增。内容哈希只负责生成技术身份，不参与状态是否应该保存的文学判断。
			item.ID = newStateItemID(characterID, field, item.Value)
			if known[item.ID] {
				return CollectionPatch[StateItem]{}, fmt.Errorf("人物 %s 的 %s 新条目与已有 ID 冲突: %s", characterID, field, item.ID)
			}
			continue
		}
		if !known[item.ID] && item.ID != newStateItemID(characterID, field, item.Value) {
			return CollectionPatch[StateItem]{}, fmt.Errorf("人物 %s 的 %s 更新引用未知 ID: %s；此 ID 不在该人物的旧集合中。若修改旧条目，准确复制其旧 ID；若正文建立了独立的新条目，id 返回空字符串，不得自行生成哈希", characterID, field, item.ID)
		}
	}
	return resolved, nil
}

// newStateItemID 不使用数组位置，因此在同一次初始化或提交重试中不会因排列变化而漂移。
// 发生极低概率哈希冲突时补丁会被上面的冲突检查拒绝，而不是静默覆盖旧内容。
func newStateItemID(characterID, field, value string) string {
	digest := sha256.Sum256([]byte(characterID + "\x00" + field + "\x00" + strings.TrimSpace(value)))
	return fmt.Sprintf("%s-%s-%x", characterID, field, digest[:8])
}

func applyCharacterPatch(current []CharacterState, patch CollectionPatch[CharacterPatch]) ([]CharacterState, error) {
	// 先校验人物层操作是否互斥。内部条目随后由同一套 CollectionPatch 规则分别校验，
	// 任一字段失败都会让整个函数返回，调用者手中的正式状态不会被部分修改。
	known, touched := map[string]bool{}, map[string]bool{}
	for _, character := range current {
		known[character.ID] = true
	}
	for _, id := range patch.Remove {
		if !known[id] || touched[id] {
			return nil, fmt.Errorf("删除了未知或重复人物: %s", id)
		}
		touched[id] = true
	}
	for _, change := range patch.Upsert {
		if !nonempty(change.ID) || !nonempty(change.Name) || touched[change.ID] {
			return nil, fmt.Errorf("人物 ID、姓名为空或重复更新: %s", change.ID)
		}
		touched[change.ID] = true
	}

	// 先保留没有被整个人物删除的旧状态及顺序，再把本章涉及的人物更新写回原位置。
	removed := map[string]bool{}
	for _, id := range patch.Remove {
		removed[id] = true
	}
	result := make([]CharacterState, 0, len(current)+len(patch.Upsert))
	positions := map[string]int{}
	for _, character := range current {
		if !removed[character.ID] {
			positions[character.ID] = len(result)
			result = append(result, character)
		}
	}
	for _, change := range patch.Upsert {
		character := CharacterState{ID: change.ID, Name: change.Name}
		if position, exists := positions[change.ID]; exists {
			character = result[position]
			character.Name = change.Name
		}
		// 三类人物状态独立应用补丁。某一类为空不会清空旧值，这正是原子 Patch 的核心语义。
		var err error
		character.Facts, err = applyStateItemPatch(character.Facts, change.Facts)
		if err != nil {
			return nil, fmt.Errorf("人物 %s 的 facts 无效: %w", change.ID, err)
		}
		character.KnowledgeAndBeliefs, err = applyStateItemPatch(character.KnowledgeAndBeliefs, change.KnowledgeAndBeliefs)
		if err != nil {
			return nil, fmt.Errorf("人物 %s 的 knowledge_and_beliefs 无效: %w", change.ID, err)
		}
		character.CommitmentsAndIntentions, err = applyStateItemPatch(character.CommitmentsAndIntentions, change.CommitmentsAndIntentions)
		if err != nil {
			return nil, fmt.Errorf("人物 %s 的 commitments_and_intentions 无效: %w", change.ID, err)
		}
		if position, exists := positions[change.ID]; exists {
			result[position] = character
		} else {
			positions[change.ID] = len(result)
			result = append(result, character)
		}
	}
	return result, nil
}

func applyStateItemPatch(current []StateItem, patch CollectionPatch[StateItem]) ([]StateItem, error) {
	return applyCollection(current, patch, func(item StateItem) string { return item.ID })
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

// ApplyChapter 是正式章节状态的推进入口。先核验 ACCEPT，再应用提取结果；
// Editor 只确认准入与完结，Commit 无权覆盖 Architect/Director 拥有的 Direction。
func ApplyChapter(current State, chapter string, commit ChapterCommit) (State, error) {
	if current.Completed || commit.Chapter != current.Chapter+1 {
		return State{}, fmt.Errorf("不能向已完成故事或错误章节提交")
	}
	if err := ValidateEditorDecision(commit.Review); err != nil {
		return State{}, err
	}
	if commit.Review.ChapterDecision != EditorAccept {
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
	next.Completed = commit.Review.StoryComplete
	next.WrittenCharacters += CountCharacters(chapter)
	next.RecentTrajectory = append(append([]TrajectoryEntry{}, current.RecentTrajectory...), TrajectoryEntry{Chapter: commit.Chapter, TrajectoryMove: commit.Result.TrajectoryEntry})
	if len(next.RecentTrajectory) > RecentTrajectoryLimit {
		next.RecentTrajectory = next.RecentTrajectory[len(next.RecentTrajectory)-RecentTrajectoryLimit:]
	}
	return next, ValidateState(next)
}

// ApplyDirectionReview 在同一章检查点上提交 Direction，不改变 Spine、章节号、正文或事实。
// KEEP 保持 Direction 与其版本；ADJUST/REPLACE 才产生新的 Direction 版本。
func ApplyDirectionReview(current State, review DirectionReview) (State, error) {
	if current.Completed || current.Chapter < 1 || review.AfterChapter != current.Chapter {
		return State{}, fmt.Errorf("不能在已完结故事或错误章节提交 Direction Review")
	}
	if current.DirectionReviewedAfterChapter >= review.AfterChapter {
		return State{}, fmt.Errorf("第 %d 章后的 Direction 已经复查", review.AfterChapter)
	}
	if err := ValidateDirectorDecision(current.Direction, review.Decision); err != nil {
		return State{}, err
	}

	next := current
	// 只更新阶段方向及复查元数据；初始化时建立的 Spine 随原检查点保留。
	next.Direction = review.Decision.Direction
	next.DirectionReviewedAfterChapter = review.AfterChapter
	if review.Decision.Action == DirectorKeep {
		if review.Version != current.DirectionVersion {
			return State{}, fmt.Errorf("KEEP 不能增加 Direction 版本")
		}
	} else {
		if review.Version != current.DirectionVersion+1 {
			return State{}, fmt.Errorf("Direction 变更必须顺序增加版本")
		}
		next.DirectionVersion = review.Version
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
		c.Facts = append([]StateItem{}, c.Facts...)
		c.KnowledgeAndBeliefs = append([]StateItem{}, c.KnowledgeAndBeliefs...)
		c.CommitmentsAndIntentions = append([]StateItem{}, c.CommitmentsAndIntentions...)
	}
	state.Relationships = append([]RelationshipState{}, state.Relationships...)
	for i := range state.Relationships {
		state.Relationships[i].Characters = append([]string{}, state.Relationships[i].Characters...)
	}
	return state
}

func cloneStatePatch(patch StatePatch) StatePatch {
	// ID 解析会改写新条目的空 ID；完整复制所有切片，保证诊断用的模型原始结果不会随之改变。
	patch.World.Upsert = append([]WorldFact{}, patch.World.Upsert...)
	patch.World.Remove = append([]string{}, patch.World.Remove...)
	patch.Characters.Upsert = append([]CharacterPatch{}, patch.Characters.Upsert...)
	patch.Characters.Remove = append([]string{}, patch.Characters.Remove...)
	for index := range patch.Characters.Upsert {
		change := &patch.Characters.Upsert[index]
		change.Facts.Upsert = append([]StateItem{}, change.Facts.Upsert...)
		change.Facts.Remove = append([]string{}, change.Facts.Remove...)
		change.KnowledgeAndBeliefs.Upsert = append([]StateItem{}, change.KnowledgeAndBeliefs.Upsert...)
		change.KnowledgeAndBeliefs.Remove = append([]string{}, change.KnowledgeAndBeliefs.Remove...)
		change.CommitmentsAndIntentions.Upsert = append([]StateItem{}, change.CommitmentsAndIntentions.Upsert...)
		change.CommitmentsAndIntentions.Remove = append([]string{}, change.CommitmentsAndIntentions.Remove...)
	}
	patch.Relationships.Upsert = append([]RelationshipState{}, patch.Relationships.Upsert...)
	patch.Relationships.Remove = append([]string{}, patch.Relationships.Remove...)
	return patch
}
