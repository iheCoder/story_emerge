package story

import (
	"reflect"
	"testing"
)

func TestCharacterPatchOnlyTouchesChangedItems(t *testing.T) {
	// 场景：人物新增一条认知、修正另一条认知，同时仍有一个本章未提及的承诺。
	// 预期：相同条目 ID 只替换目标认知；空 ID 的新认知由程序补全；旧承诺因未出现在 Patch 中而保留。
	before := CurrentStoryState{
		World: []WorldFact{{ID: "missing", Description: "某人失踪"}, {ID: "closed", Description: "道路封闭"}},
		Characters: []CharacterState{{
			ID:   "a",
			Name: "甲",
			Facts: []StateItem{
				{ID: "a-fact-resident", Value: "住在旧城"},
				{ID: "a-fact-job", Value: "仍在车站工作"},
			},
			KnowledgeAndBeliefs: []StateItem{
				{ID: "a-belief-location", Value: "以为对方仍在城里"},
			},
			CommitmentsAndIntentions: []StateItem{{ID: "a-commitment-wait", Value: "答应继续等候消息"}},
		}},
	}
	patch := StatePatch{
		World: CollectionPatch[WorldFact]{Upsert: []WorldFact{{ID: "missing", Description: "已经找到当事人"}}, Remove: []string{"closed"}},
		Characters: CollectionPatch[CharacterPatch]{Upsert: []CharacterPatch{
			{
				ID:    "a",
				Name:  "甲",
				Facts: CollectionPatch[StateItem]{Remove: []string{"a-fact-job"}},
				KnowledgeAndBeliefs: CollectionPatch[StateItem]{Upsert: []StateItem{
					{ID: "a-belief-location", Value: "确认对方已经离开城里"},
					{ID: "", Value: "怀疑乙隐瞒了行踪"},
				}},
			},
			{ID: "b", Name: "乙"},
		}},
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
	if len(next.Characters[0].KnowledgeAndBeliefs) != 2 || next.Characters[0].KnowledgeAndBeliefs[0].Value != "确认对方已经离开城里" {
		t.Fatalf("人物认知没有按条目更新: %#v", next.Characters[0].KnowledgeAndBeliefs)
	}
	if next.Characters[0].KnowledgeAndBeliefs[1].ID == "" || len(next.Characters[0].CommitmentsAndIntentions) != 1 {
		t.Fatalf("新增条目没有 ID，或未触碰的承诺丢失: %#v", next.Characters[0])
	}
	if len(next.Characters[0].Facts) != 1 || next.Characters[0].Facts[0].ID != "a-fact-resident" {
		t.Fatalf("人物事实没有按 remove 精确删除: %#v", next.Characters[0].Facts)
	}
	next.Characters[0].KnowledgeAndBeliefs[0].Value = "外部修改"
	if before.Characters[0].KnowledgeAndBeliefs[0].Value != "以为对方仍在城里" || patch.Characters.Upsert[0].KnowledgeAndBeliefs.Upsert[0].Value != "确认对方已经离开城里" {
		t.Fatal("结果与旧状态或补丁共享可变数组")
	}
}

func TestStateItemIDsAreStableAcrossResolutionAndRetry(t *testing.T) {
	// 场景：Architect 产生无 ID 的初始条目，Commit 随后产生无 ID 的新增条目；同一提交可能因落盘失败而重试。
	// 预期：程序补全所有 ID，并且同样输入每次解析得到相同 ID，避免重试产生第二份逻辑相同的状态。
	initial := CurrentStoryState{Characters: []CharacterState{{ID: "a", Name: "甲", Facts: []StateItem{{Value: "住在旧城"}}}}}
	resolvedInitial, err := ResolveInitialStateItemIDs(initial)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedInitial.Characters[0].Facts[0].ID == "" {
		t.Fatal("初始人物条目没有生成 ID")
	}

	patch := StatePatch{Characters: CollectionPatch[CharacterPatch]{Upsert: []CharacterPatch{{
		ID: "a", Name: "甲", KnowledgeAndBeliefs: CollectionPatch[StateItem]{Upsert: []StateItem{{Value: "知道道路已经封闭"}}},
	}}}}
	first, err := ResolveStatePatchIDs(resolvedInitial, patch)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveStatePatchIDs(resolvedInitial, patch)
	if err != nil {
		t.Fatal(err)
	}
	firstID := first.Characters.Upsert[0].KnowledgeAndBeliefs.Upsert[0].ID
	secondID := second.Characters.Upsert[0].KnowledgeAndBeliefs.Upsert[0].ID
	if firstID == "" || firstID != secondID {
		t.Fatalf("相同提交重试产生了不同 ID: %q != %q", firstID, secondID)
	}
}

func TestInvalidPatchCannotMutateCommittedState(t *testing.T) {
	// 场景：移除人物时仍留着引用它的关系，或同时更新/移除同一事实。
	// 预期：整个补丁失败，已经提交的状态逐字段不变。
	before := CurrentStoryState{Characters: []CharacterState{{ID: "a", Name: "甲"}, {ID: "b", Name: "乙"}}, Relationships: []RelationshipState{{ID: "ab", Characters: []string{"a", "b"}, Description: "朋友"}}}
	saved := cloneStoryState(before)
	for _, patch := range []StatePatch{
		{Characters: CollectionPatch[CharacterPatch]{Remove: []string{"b"}}},
		{World: CollectionPatch[WorldFact]{Remove: []string{"unknown"}}},
		{Characters: CollectionPatch[CharacterPatch]{Upsert: []CharacterPatch{{ID: "a", Name: "甲"}}, Remove: []string{"a"}}},
		{Characters: CollectionPatch[CharacterPatch]{Upsert: []CharacterPatch{{ID: "a", Name: "甲"}, {ID: "a", Name: "另一个甲"}}}},
		{Characters: CollectionPatch[CharacterPatch]{Upsert: []CharacterPatch{{ID: "a", Name: "甲", Facts: CollectionPatch[StateItem]{Upsert: []StateItem{{ID: "unknown", Value: "未知引用"}}}}}}},
	} {
		if _, err := ApplyPatch(before, patch); err == nil {
			t.Fatal("非法补丁被接受")
		}
		if !reflect.DeepEqual(cloneStoryState(before), saved) {
			t.Fatal("失败补丁污染了旧状态")
		}
	}
	// 同时删除对应关系即可合法移除人物，不要求保留已经失效的历史条目。
	_, err := ApplyPatch(before, StatePatch{Characters: CollectionPatch[CharacterPatch]{Remove: []string{"b"}}, Relationships: CollectionPatch[RelationshipState]{Remove: []string{"ab"}}})
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
	current := State{StorySpine: testSpine(), Direction: Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "共同生活", DesiredShift: "逐渐建立信任", ReaderExpectation: "读者等待信任如何形成"}, DirectionVersion: 1}
	for number := 1; number <= 7; number++ {
		commit := ChapterCommit{Chapter: number, Title: "日常", Review: EditorDecision{ChapterDecision: EditorAccept, Assessment: EditorAssessment{Contribution: "呈现陪伴", Sequence: "继续积累关系", Execution: "日常可信"}}, Result: CommitResult{ChapterSummary: "一起做饭", TrajectoryEntry: TrajectoryMove{StoryMove: "现实局势未变，呈现陪伴", NarrativeShape: "做饭 → 交谈"}}}
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
	decision := EditorDecision{ChapterDecision: EditorRevise, Assessment: EditorAssessment{Contribution: "无贡献", Sequence: "重复", Execution: "需要重写"}, BlockingIssues: []string{"需要重新考虑"}, StoryComplete: true}
	if ValidateEditorDecision(decision) == nil {
		t.Fatal("退回稿件确认了完结")
	}
	decision = EditorDecision{ChapterDecision: EditorAccept, Assessment: EditorAssessment{Contribution: "核心承诺已兑现", Sequence: "形成落点", Execution: "正文成立"}, StoryComplete: true}
	if err := ValidateEditorDecision(decision); err != nil {
		t.Fatal(err)
	}
}

func TestDirectionReviewChangesOnlyDirectionMetadata(t *testing.T) {
	// 场景：第三章提交后 Director 调整阶段方向。
	// 预期：章节、事实、字数与轨迹完全不变，只更新 Direction、版本和复查章节。
	current := State{
		Chapter: 3, StorySpine: testSpine(), Direction: Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "建立信任", DesiredShift: "形成合作", ReaderExpectation: "读者等待两人能否合作"},
		DirectionVersion: 1, RecentTrajectory: []TrajectoryEntry{
			{Chapter: 1, TrajectoryMove: TrajectoryMove{StoryMove: "相识", NarrativeShape: "见面"}},
			{Chapter: 2, TrajectoryMove: TrajectoryMove{StoryMove: "试探", NarrativeShape: "交谈"}},
			{Chapter: 3, TrajectoryMove: TrajectoryMove{StoryMove: "合作", NarrativeShape: "共同处理问题"}},
		}, WrittenCharacters: 1200,
	}
	updated := Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "承担后果", DesiredShift: "从合作转向共同负责", ReaderExpectation: "读者等待合作如何经受代价"}
	next, err := ApplyDirectionReview(current, DirectionReview{AfterChapter: 3, Version: 2, Decision: DirectorDecision{Action: DirectorAdjust, Direction: updated, Reason: "合作已经形成"}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Chapter != current.Chapter || next.WrittenCharacters != current.WrittenCharacters || !reflect.DeepEqual(next.Story, current.Story) || !reflect.DeepEqual(next.RecentTrajectory, current.RecentTrajectory) {
		t.Fatalf("Direction Review 越权修改章节事实: %#v", next)
	}
	if next.Direction != updated || next.DirectionVersion != 2 || next.DirectionReviewedAfterChapter != 3 {
		t.Fatalf("Direction Review 未正确生效: %#v", next)
	}
}
