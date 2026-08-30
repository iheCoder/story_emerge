package story

import "testing"

func TestApplyDeltaAdvancesSnapshotWithoutMutatingCurrent(t *testing.T) {
	// 场景：用合法第 1 章 delta 应用到第 0 章快照。
	// 预期：返回的新快照推进章节、追加事实并更新人物情绪，而输入 current 完全不变，
	// 验证 ApplyDelta 的 validate-then-clone 事务语义。
	// 准备第 0 章状态和合法第 1 章 delta。
	t.Parallel()
	current := sampleState()
	delta := sampleDelta()

	// 应用变更并确认旧状态保持不变。
	next, err := ApplyDelta(current, delta)
	if err != nil {
		t.Fatal(err)
	}
	if current.Chapter != 0 || len(current.Facts) != 1 {
		t.Fatalf("current state was mutated: %#v", current)
	}

	// 检查新快照的章节、事实和人物动态。
	if next.Chapter != 1 || len(next.Facts) != 2 || next.Characters[0].Emotion != "振奋" {
		t.Fatalf("unexpected next state: %#v", next)
	}
}

func TestApplyDeltaMergesKnowledgeWithoutRepeatingHistory(t *testing.T) {
	// 场景：本章再次提交旧事实 fired 的认知，并新增 machine_fixed 认知。
	// 预期：旧知识只保留一份并追加新知识，验证 FactID 幂等合并不会重复历史。
	// 准备包含旧事实和新事实知识的 delta。
	t.Parallel()
	delta := sampleDelta()
	delta.CharacterStates[0].Knowledge = []Knowledge{{FactID: "machine_fixed", Belief: "knows", Confidence: 100}}

	// 应用并检查知识合并结果。
	next, err := ApplyDelta(sampleState(), delta)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Characters[0].Knowledge) != 2 || next.Characters[0].Knowledge[0].FactID != "fired" {
		t.Fatalf("旧知识应被自动保留并合并新知识，实际为 %#v", next.Characters[0].Knowledge)
	}
}

func TestDropUnknownRelationshipsKeepsOnlyCanonicalCharacters(t *testing.T) {
	// 场景：模型为临时门卫写入关系，但当前正式人物表没有该 ID。
	// 预期：关系被移除并返回诊断项，验证清理长期状态时不影响事实/时间线。
	// 构造指向临时人物的关系增量。
	t.Parallel()
	delta := sampleDelta()
	delta.CharacterStates[0].Relationships = []Relationship{
		{TargetID: "minor_guard", Note: "临时门卫"},
	}

	// 执行归一化并检查过滤结果与诊断。
	normalized, dropped := DropUnknownRelationships(sampleState(), delta)
	if len(dropped) != 1 || len(normalized.CharacterStates[0].Relationships) != 0 {
		t.Fatalf("unexpected normalization: %#v, %#v", normalized, dropped)
	}
}

func TestDropDuplicatedNewThreadUpdatesKeepsNewDefinition(t *testing.T) {
	// 场景：同一新剧情线同时出现在 thread_states 和 new_threads。
	// 预期：删除无效更新、保留完整的新线定义，并记录被删 ID，验证归类修复规则。
	// 让同一条新剧情线同时出现在两个字段。
	current := sampleState()
	created := PlotThreadState{ID: "new-clue", Name: "新线索", Status: "active", OpenedChapter: 2}
	delta := StateDelta{
		ThreadStates: []PlotThreadState{created},
		NewThreads:   []PlotThreadState{created},
	}

	// 归一化并验证只保留 new_threads 定义。
	normalized, dropped := DropDuplicatedNewThreadUpdates(current, delta)
	if len(dropped) != 1 || dropped[0] != "new-clue" {
		t.Fatalf("应记录被移除的重复更新，实际为 %#v", dropped)
	}
	if len(normalized.ThreadStates) != 0 || len(normalized.NewThreads) != 1 {
		t.Fatalf("应仅保留新剧情线定义，实际为 %#v", normalized)
	}
}

func TestDropUnknownCharacterStatesPreservesCanonicalOnly(t *testing.T) {
	// 场景：delta 同时包含正式主角和一次性线索人物的状态。
	// 预期：只保留主角状态并报告线索人物，验证长期人物表不会无限膨胀。
	// 在合法人物状态之外追加一次性人物。
	delta := sampleDelta()
	delta.CharacterStates = append(delta.CharacterStates, CharacterState{CharacterID: "minor_witness"})

	// 执行白名单过滤并检查正式人物未受影响。
	normalized, dropped := DropUnknownCharacterStates(sampleState(), delta)
	if len(dropped) != 1 || dropped[0] != "minor_witness" {
		t.Fatalf("应记录被移除的临时人物，实际为 %#v", dropped)
	}
	if len(normalized.CharacterStates) != 1 || normalized.CharacterStates[0].CharacterID != "hero" {
		t.Fatalf("核心人物状态不应受影响，实际为 %#v", normalized.CharacterStates)
	}
}

func sampleState() State {
	// 构造带一条既有事实、人物知识和主线的第 0 章快照，覆盖合并测试所需的引用关系。
	return State{
		Chapter: 0,
		Characters: []CharacterState{{
			CharacterID: "hero", Goal: "修机器", Emotion: "压抑", Location: "工厂",
			Knowledge:     []Knowledge{{FactID: "fired", Belief: "knows", Confidence: 100}},
			Relationships: []Relationship{},
		}},
		Facts: []Fact{{ID: "fired", Description: "顾川被开除", Visibility: "public", SinceChapter: 0}},
		Threads: []PlotThreadState{{
			ID: "comeback", Name: "翻身", Kind: "main", Status: "active", Progress: "刚开始",
			OpenedChapter: 0, LastTouchedChapter: 0, PlannedPayoffChapter: 3,
		}},
	}
}

func sampleDelta() StateDelta {
	// 构造可直接 ApplyDelta 的第 1 章变更：新增事实、人物动态、剧情线推进和时间事件。
	return StateDelta{
		Chapter:  1,
		Summary:  ChapterSummary{Number: 1, Title: "旧机床", Summary: "顾川修好机床", KeyChanges: []string{"获得机会"}},
		NewFacts: []Fact{{ID: "machine_fixed", Description: "机床修复", Visibility: "public", SinceChapter: 1}},
		CharacterStates: []CharacterState{{
			CharacterID: "hero", Goal: "接下订单", Emotion: "振奋", Location: "车间",
			Knowledge:     []Knowledge{{FactID: "fired", Belief: "knows", Confidence: 100}},
			Relationships: []Relationship{},
		}},
		ThreadStates: []PlotThreadState{{
			ID: "comeback", Name: "翻身", Kind: "main", Status: "active", Progress: "修好第一台机器",
			OpenedChapter: 0, LastTouchedChapter: 1, PlannedPayoffChapter: 3,
		}},
		NewThreads:     []PlotThreadState{},
		TimelineEvents: []TimelineEvent{{Chapter: 1, Description: "修好机床", Participants: []string{"hero"}}},
	}
}
