package story

import (
	"reflect"
	"testing"
)

func TestPatchReplacesCurrentFactsAndSupportsNewCharacters(t *testing.T) {
	// 场景：旧失踪状态已失效，人物获知新消息，并与本章新出现的人物形成关系。
	// 预期：相同 ID 替换当前值，失效事实移除；新人物与关系同批提交，不积累矛盾历史。
	before := CurrentStoryState{
		World:      []WorldFact{{ID: "missing", Description: "某人失踪"}, {ID: "closed", Description: "道路封闭"}},
		Characters: []CharacterState{{ID: "a", Name: "甲", KnowledgeAndBeliefs: []string{"以为对方仍在城里"}}},
	}
	patch := StatePatch{
		World:         CollectionPatch[WorldFact]{Upsert: []WorldFact{{ID: "missing", Description: "已经找到当事人"}}, Remove: []string{"closed"}},
		Characters:    CollectionPatch[CharacterState]{Upsert: []CharacterState{{ID: "a", Name: "甲", KnowledgeAndBeliefs: []string{"怀疑乙隐瞒了行踪"}}, {ID: "b", Name: "乙"}}},
		Relationships: CollectionPatch[RelationshipState]{Upsert: []RelationshipState{{ID: "ab", Characters: []string{"a", "b"}, Description: "开始相互试探"}}},
	}
	next, err := ApplyPatch(before, patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.World) != 1 || next.World[0].Description != "已经找到当事人" {
		t.Fatalf("状态未替换: %#v", next.World)
	}
	if len(next.Characters) != 2 || len(next.Relationships) != 1 {
		t.Fatalf("新增人物与关系丢失: %#v", next)
	}
	if len(next.Characters[0].KnowledgeAndBeliefs) != 1 {
		t.Fatal("认知数组被追加成历史")
	}
	next.Characters[0].KnowledgeAndBeliefs[0] = "外部修改"
	if before.Characters[0].KnowledgeAndBeliefs[0] != "以为对方仍在城里" || patch.Characters.Upsert[0].KnowledgeAndBeliefs[0] != "怀疑乙隐瞒了行踪" {
		t.Fatal("结果与旧状态或补丁共享可变数组")
	}
}

func TestInvalidPatchCannotMutateCommittedState(t *testing.T) {
	// 场景：移除人物时仍留着引用它的关系，或同时更新/移除同一事实。
	// 预期：整个补丁失败，已经提交的状态逐字段不变。
	before := CurrentStoryState{Characters: []CharacterState{{ID: "a", Name: "甲"}, {ID: "b", Name: "乙"}}, Relationships: []RelationshipState{{ID: "ab", Characters: []string{"a", "b"}, Description: "朋友"}}}
	saved := cloneStoryState(before)
	for _, patch := range []StatePatch{
		{Characters: CollectionPatch[CharacterState]{Remove: []string{"b"}}},
		{World: CollectionPatch[WorldFact]{Remove: []string{"unknown"}}},
		{Characters: CollectionPatch[CharacterState]{Upsert: []CharacterState{{ID: "a", Name: "甲"}}, Remove: []string{"a"}}},
		{Characters: CollectionPatch[CharacterState]{Upsert: []CharacterState{{ID: "a", Name: "甲"}, {ID: "a", Name: "另一个甲"}}}},
	} {
		if _, err := ApplyPatch(before, patch); err == nil {
			t.Fatal("非法补丁被接受")
		}
		if !reflect.DeepEqual(cloneStoryState(before), saved) {
			t.Fatal("失败补丁污染了旧状态")
		}
	}
	// 同时删除对应关系即可合法移除人物，不要求保留已经失效的历史条目。
	_, err := ApplyPatch(before, StatePatch{Characters: CollectionPatch[CharacterState]{Remove: []string{"b"}}, Relationships: CollectionPatch[RelationshipState]{Remove: []string{"ab"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRelationshipIntegrityStillRejectsInvalidIDs(t *testing.T) {
	// 场景：取消参与者人数限制后，关系仍可能包含悬空引用、重复引用或重复条目 ID。
	// 预期：只放宽人数，不放宽结构完整性；每个场景从独立的合法单参与者状态开始。
	for _, scenario := range []string{"未知人物引用", "重复人物引用", "重复关系ID", "重复人物ID"} {
		t.Run(scenario, func(t *testing.T) {
			state := CurrentStoryState{
				Characters:    []CharacterState{{ID: "a", Name: "甲"}},
				Relationships: []RelationshipState{{ID: "r", Characters: []string{"a"}, Description: "甲开始信任夜班保安"}},
			}
			if err := ValidateStoryState(state); err != nil {
				t.Fatalf("合法前置状态被拒绝: %v", err)
			}

			// 每次只破坏一种 ID 约束，防止其他错误掩盖被测规则是否仍然生效。
			switch scenario {
			case "未知人物引用":
				state.Relationships[0].Characters = []string{"missing"}
			case "重复人物引用":
				state.Relationships[0].Characters = []string{"a", "a"}
			case "重复关系ID":
				state.Relationships = append(state.Relationships, state.Relationships[0])
			case "重复人物ID":
				state.Characters = append(state.Characters, state.Characters[0])
			}
			if err := ValidateStoryState(state); err == nil {
				t.Fatal("无效 ID 被接受")
			}
		})
	}
}

func TestQuietChaptersKeepFactsAndRollTrajectory(t *testing.T) {
	// 场景：连续七章只承担情绪/日常功能，没有事实补丁。
	// 预期：空补丁合法；只留最后五章轨迹；字数再多也不自行完结。
	current := State{Direction: Direction{Focus: "共同生活", DesiredShift: "逐渐建立信任"}}
	for number := 1; number <= 7; number++ {
		commit := ChapterCommit{Chapter: number, Title: "日常", Plan: ChapterPlan{DirectionAction: "KEEP", ChapterIntent: ChapterIntent{IntendedEffect: "感受陪伴", WhyNow: "承接前一章情绪"}}, Review: EditorDecision{Action: EditorAccept, Reason: "日常可信"}, Result: CommitResult{ChapterSummary: "一起做饭", TrajectoryEntry: TrajectoryMove{StoryMove: "现实局势未变，呈现陪伴", NarrativeShape: "做饭 → 交谈"}}}
		next, err := ApplyChapter(current, "# 第1章 日常\n\n甲和乙一起做饭。", commit)
		if err != nil {
			t.Fatal(err)
		}
		current = next
	}
	if len(current.RecentTrajectory) != 5 || current.RecentTrajectory[0].Chapter != 3 || current.RecentTrajectory[4].Chapter != 7 {
		t.Fatalf("轨迹窗口错误: %#v", current.RecentTrajectory)
	}
	if current.Completed || current.WrittenCharacters != 49 {
		t.Fatalf("字数或完结规则错误: %#v", current)
	}
}

func TestOnlyAcceptedEditorCanConfirmCompletion(t *testing.T) {
	// 场景：模型在退回正文的同时宣称故事结束。
	// 预期：输出被拒绝；只有明确 ACCEPT 才有权确认完结。
	decision := EditorDecision{Action: EditorReplan, Reason: "意图不成立", BlockingIssues: []string{"需要重新考虑"}, StoryComplete: true}
	if ValidateEditorDecision(decision) == nil {
		t.Fatal("退回稿件确认了完结")
	}
	decision = EditorDecision{Action: EditorAccept, Reason: "核心承诺已兑现", StoryComplete: true}
	if err := ValidateEditorDecision(decision); err != nil {
		t.Fatal(err)
	}
}
