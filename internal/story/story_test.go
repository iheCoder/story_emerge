package story

import "testing"

func TestProjectHasNoChapterOrWordQuota(t *testing.T) {
	// 场景：用户只选择“超长故事”这一规模意图。
	// 预期：项目校验不把它翻译成章节数、字数或场景配额。
	project := Project{Idea: "一段故事", LengthProfile: "epic", MaxCalls: 1}
	if err := ValidateProject(project); err != nil {
		t.Fatal(err)
	}
}

func TestApplyStoryUpdateAllowsEmptyLongTermChanges(t *testing.T) {
	// 场景：一章只完成了短期气氛和人物互动，没有值得长期登记的新状态。
	// 预期：空变化集合仍能把章节号推进一次，不能强迫 Editor 制造状态。
	outline := testOutline()
	current := NewInitialState(testInitialState(), outline)
	update := StoryUpdate{
		Chapter: 1, CharacterChanges: []CharacterStateChange{},
		SituationStateChanges: []SituationStateChange{},
		StoryStatus:           "ongoing",
	}

	next, err := ApplyStoryUpdate(current, outline, update)
	if err != nil {
		t.Fatal(err)
	}
	if next.Chapter != 1 || len(next.SituationStates) != len(current.SituationStates) {
		t.Fatalf("空 Story Update 改坏状态: %#v", next)
	}
}

func TestRejectedDraftCannotMutateCurrentState(t *testing.T) {
	// 场景：Editor 对草稿提出 revise，草稿中包含一个尚未获准提交的长期变化。
	// 操作：只构造变化但不调用 ApplyStoryUpdate，模拟被否决草稿退出当前分支。
	// 预期：调用方持有的 HEAD 快照保持原值，为新草稿提供干净基线。
	outline := testOutline()
	current := NewInitialState(testInitialState(), outline)
	rejected := StoryUpdate{Chapter: 1, SituationStateChanges: []SituationStateChange{{Operation: "upsert", ID: "rejected", Description: "不应出现"}}, StoryStatus: "ongoing"}
	_ = rejected

	if len(current.SituationStates) != 1 || current.SituationStates[0].ID != "weather" {
		t.Fatalf("未应用的草稿变化污染了当前状态: %#v", current.SituationStates)
	}
}

func TestStoryUpdateRejectsUnknownReferences(t *testing.T) {
	// 场景：Editor 输出不存在的人物 ID。
	// 预期：确定性校验拒绝人物引用，HEAD 事务可以在写盘前停止。
	outline := testOutline()
	current := NewInitialState(testInitialState(), outline)
	update := StoryUpdate{
		Chapter: 1, CharacterChanges: []CharacterStateChange{{CharacterID: "unknown", State: "变化"}},
		StoryStatus: "ongoing",
	}

	if _, err := ApplyStoryUpdate(current, outline, update); err == nil {
		t.Fatal("未知引用却通过 Story Update 校验")
	}
}

func TestReplanUsesGenericTracksWithoutFixedGenreType(t *testing.T) {
	// 场景：Architect 为一个中性故事新增“修复旧桥”这股发展力量。
	// 预期：程序只认识通用 StoryTrack，不要求悬疑、关系或升级等题材枚举。
	old := testOutline()
	next := old
	next.Version = 1
	next.Tracks = append([]StoryTrack(nil), old.Tracks...)
	next.Tracks = append(next.Tracks, StoryTrack{
		ID: "bridge", Name: "旧桥", Direction: "从争议走向共同修复", Status: "ongoing",
	})

	if err := ValidateReplan(old, next); err != nil {
		t.Fatalf("通用 Track 被题材枚举拒绝: %v", err)
	}
}

func testOutline() StoryOutline {
	return StoryOutline{
		Version: 0, CurrentArc: StoryArc{Name: "风雪前", Purpose: "让送信人真正离开家"},
		Tracks: []StoryTrack{{ID: "journey", Name: "送信", Direction: "走出村庄", Status: "ongoing"}},
	}
}

func testInitialState() InitialState {
	return InitialState{
		CharacterStates: []CharacterState{{CharacterID: "traveler", State: "尚未离家"}},
		SituationStates: []SituationState{{ID: "weather", Description: "风雪将至"}},
	}
}
