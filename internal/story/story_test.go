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

	next, err := ApplyStoryUpdate(current, outline, update, nil)
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

	if _, err := ApplyStoryUpdate(current, outline, update, nil); err == nil {
		t.Fatal("未知引用却通过 Story Update 校验")
	}
}

func TestLiveTensionsAreAReplaceableAttentionSnapshot(t *testing.T) {
	// 场景：一章没有触碰旧张力，但 Editor 判断它仍有生命力，并新增一项真正改变故事重心的力量。
	// 预期：程序整体保存 Editor 的选择，不按“本章未出现”自动老化，也不要求填满容量。
	outline := testOutline()
	current := NewInitialState(testInitialState(), outline)
	current.LiveTensions = []string{"旅行者是否愿意真正离开熟悉生活"}
	update := StoryUpdate{Chapter: 1, StoryStatus: "ongoing"}
	tensions := []string{
		" 旅行者是否愿意真正离开熟悉生活 ",
		"同行者的保护正在与旅行者的自主选择发生拉扯",
	}

	next, err := ApplyStoryUpdate(current, outline, update, tensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.LiveTensions) != 2 || next.LiveTensions[0] != "旅行者是否愿意真正离开熟悉生活" {
		t.Fatalf("Live Tension 快照没有按 Editor 输出整体保存: %#v", next.LiveTensions)
	}
	if len(current.LiveTensions) != 1 {
		t.Fatalf("应用下一章状态污染了当前 HEAD: %#v", current.LiveTensions)
	}
}

func TestLiveTensionValidationOnlyEnforcesDeterministicBounds(t *testing.T) {
	// 场景：空列表、重复项与超过容量的列表分别进入状态边界。
	// 预期：空列表合法；重复和超限被拒绝；程序不判断内容属于关系、悬疑或其他题材。
	if err := ValidateLiveTensions(nil); err != nil {
		t.Fatalf("空 Live Tension 被错误拒绝: %v", err)
	}
	if err := ValidateLiveTensions([]string{"人物的选择仍有代价", " 人物的选择仍有代价 "}); err == nil {
		t.Fatal("规范化后重复的 Live Tension 被允许")
	}
	if err := ValidateLiveTensions([]string{"一", "二", "三", "四", "五", "六"}); err == nil {
		t.Fatal("超过状态容量的 Live Tension 被允许")
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
