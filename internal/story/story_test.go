package story

import "testing"

func TestProjectHasNoChapterOrWordQuota(t *testing.T) {
	project := Project{Idea: "一段故事", LengthProfile: "epic", MaxCalls: 1}
	if err := ValidateProject(project); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDeltaTracksNaturalCompletion(t *testing.T) {
	outline := StoryOutline{Version: 0, CoreConflict: "冲突", CurrentMovementID: "m1",
		Movements: []StoryMovement{{ID: "m1"}}}
	current := NewInitialState(InitialState{}, outline)
	delta := StateDelta{Chapter: 1, Summary: ChapterSummary{Number: 1}, OutlineProgress: OutlineProgress{
		CurrentMovementID: "m1", Status: "completed", Evidence: "正文已经完成"}, StoryStatus: "ongoing"}
	next, err := ApplyDelta(current, delta)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.CompletedMovementIDs) != 1 || next.CompletedMovementIDs[0] != "m1" {
		t.Fatalf("完成阶段未进入账本: %#v", next.CompletedMovementIDs)
	}
}

func TestReplanCannotRewriteCompletedMovement(t *testing.T) {
	old := StoryOutline{CoreConflict: "冲突", CurrentMovementID: "m1",
		Movements: []StoryMovement{{ID: "m1", Name: "已经发生"}, {ID: "m2"}}}
	next := old
	next.CurrentMovementID = "m2"
	next.Movements = append([]StoryMovement(nil), old.Movements...)
	next.Movements[0].Name = "偷偷改写"
	if err := ValidateReplan(old, next, []string{"m1"}); err == nil {
		t.Fatal("重规划改写已完成历史却未被拒绝")
	}
}

func TestGenesisRejectsBroadAudienceLabel(t *testing.T) {
	genesis := Genesis{Bible: StoryBible{Title: "书", ProtagonistID: "p", EndingDirection: "结局",
		Characters: []Character{{ID: "p", Name: "主角"}}, TargetReader: TargetReader{
			Name: "青少年读者", ReadingHistory: "看网络小说", Craves: []string{"刺激"}, DropsWhen: []string{"慢"}, BingeTriggers: []string{"反转"}},
		NarrativePromise: NarrativePromise{PrimaryPleasure: "刺激", MustDeliver: []string{"兑现"}, MustNotBecome: []string{"流水账"}}},
		Outline:      StoryOutline{CoreConflict: "冲突", CurrentMovementID: "m", Movements: []StoryMovement{{ID: "m"}}},
		InitialState: InitialState{Characters: []CharacterState{{CharacterID: "p"}}}, InitialReaderState: ReaderState{Chapter: 0, SuggestedAction: "continue"}}
	if err := ValidateGenesis(genesis); err == nil {
		t.Fatal("宽泛人口标签不应通过目标读者验证")
	}
}

func TestApplyDeltaAllowsAnyNumberOfKnownCharacters(t *testing.T) {
	// 场景：群像章节同时改变五名已经登记的人物。
	// 预期：状态层只验证人物 ID 和引用，不用隐藏配额限制文学内容。
	characters := make([]CharacterState, 5)
	changes := make([]CharacterState, 5)
	for index := range characters {
		id := string(rune('a' + index))
		characters[index] = CharacterState{CharacterID: id}
		changes[index] = CharacterState{CharacterID: id, Emotion: "变化"}
	}
	current := NewInitialState(InitialState{Characters: characters}, StoryOutline{
		Version: 0, CurrentMovementID: "m", Movements: []StoryMovement{{ID: "m"}},
	})
	delta := StateDelta{
		Chapter: 1, Summary: ChapterSummary{Number: 1}, CharacterStates: changes,
		OutlineProgress: OutlineProgress{CurrentMovementID: "m", Status: "ongoing"}, StoryStatus: "ongoing",
	}
	if _, err := ApplyDelta(current, delta); err != nil {
		t.Fatalf("正式人物数量被误当成章节配额: %v", err)
	}
}
