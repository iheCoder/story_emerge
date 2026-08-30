package story

import "testing"

func TestApplyDeltaAdvancesSnapshotWithoutMutatingCurrent(t *testing.T) {
	t.Parallel()
	current := sampleState()
	delta := sampleDelta()
	next, err := ApplyDelta(current, delta)
	if err != nil {
		t.Fatal(err)
	}
	if current.Chapter != 0 || len(current.Facts) != 1 {
		t.Fatalf("current state was mutated: %#v", current)
	}
	if next.Chapter != 1 || len(next.Facts) != 2 || next.Characters[0].Emotion != "振奋" {
		t.Fatalf("unexpected next state: %#v", next)
	}
}

func TestApplyDeltaMergesKnowledgeWithoutRepeatingHistory(t *testing.T) {
	t.Parallel()
	delta := sampleDelta()
	delta.CharacterStates[0].Knowledge = []Knowledge{{FactID: "machine_fixed", Belief: "knows", Confidence: 100}}
	next, err := ApplyDelta(sampleState(), delta)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Characters[0].Knowledge) != 2 || next.Characters[0].Knowledge[0].FactID != "fired" {
		t.Fatalf("旧知识应被自动保留并合并新知识，实际为 %#v", next.Characters[0].Knowledge)
	}
}

func TestDropUnknownRelationshipsKeepsOnlyCanonicalCharacters(t *testing.T) {
	t.Parallel()
	delta := sampleDelta()
	delta.CharacterStates[0].Relationships = []Relationship{
		{TargetID: "minor_guard", Note: "临时门卫"},
	}
	normalized, dropped := DropUnknownRelationships(sampleState(), delta)
	if len(dropped) != 1 || len(normalized.CharacterStates[0].Relationships) != 0 {
		t.Fatalf("unexpected normalization: %#v, %#v", normalized, dropped)
	}
}

func TestDropDuplicatedNewThreadUpdatesKeepsNewDefinition(t *testing.T) {
	current := sampleState()
	created := PlotThreadState{ID: "new-clue", Name: "新线索", Status: "active", OpenedChapter: 2}
	delta := StateDelta{
		ThreadStates: []PlotThreadState{created},
		NewThreads:   []PlotThreadState{created},
	}

	normalized, dropped := DropDuplicatedNewThreadUpdates(current, delta)
	if len(dropped) != 1 || dropped[0] != "new-clue" {
		t.Fatalf("应记录被移除的重复更新，实际为 %#v", dropped)
	}
	if len(normalized.ThreadStates) != 0 || len(normalized.NewThreads) != 1 {
		t.Fatalf("应仅保留新剧情线定义，实际为 %#v", normalized)
	}
}

func TestDropUnknownCharacterStatesPreservesCanonicalOnly(t *testing.T) {
	delta := sampleDelta()
	delta.CharacterStates = append(delta.CharacterStates, CharacterState{CharacterID: "minor_witness"})

	normalized, dropped := DropUnknownCharacterStates(sampleState(), delta)
	if len(dropped) != 1 || dropped[0] != "minor_witness" {
		t.Fatalf("应记录被移除的临时人物，实际为 %#v", dropped)
	}
	if len(normalized.CharacterStates) != 1 || normalized.CharacterStates[0].CharacterID != "hero" {
		t.Fatalf("核心人物状态不应受影响，实际为 %#v", normalized.CharacterStates)
	}
}

func sampleState() State {
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
