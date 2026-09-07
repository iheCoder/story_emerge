package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

type scriptedGenerator struct {
	responses map[string]string
	failures  map[string]error
	requests  map[string]llm.Request
	order     []string
}

func (fake *scriptedGenerator) Generate(_ context.Context, request llm.Request) (llm.Result, error) {
	fake.requests[request.Stage] = request
	fake.order = append(fake.order, request.Stage)
	if err := fake.failures[request.Stage]; err != nil {
		return llm.Result{}, err
	}
	output, ok := fake.responses[request.Stage]
	if !ok {
		return llm.Result{}, fmt.Errorf("unexpected stage %s", request.Stage)
	}
	return llm.Result{Text: output, Model: "fake", ResponseID: request.Stage}, nil
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func testDirection() story.Direction {
	return story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立",
		Focus: "DIRECTION_FOR_WRITER", DesiredShift: "自然建立信任",
		ReaderExpectation: "读者等待看到两人是否愿意共同承担选择",
	}
}

func testGenesis() story.Genesis {
	return story.Genesis{
		Title: "测试故事", StoryCore: story.StoryCore{
			StoryEngine:        story.StoryEngine{Loop: "人物回应事件，回应改变处境", ProgressionAxis: "逐渐承担选择"},
			ReaderPromises:     []story.ReaderPromise{{Promise: "信任", PayoffShape: "共同经历"}, {Promise: "责任", PayoffShape: "选择的结果"}, {Promise: "旅程", PayoffShape: "找到自己的落点"}},
			ExperienceContract: story.ExperienceContract{TargetExperience: "普通人的生活", NarrativePrinciples: []string{"通过场景呈现"}, DriftBoundaries: []string{"连续解释"}},
		},
		InitialStoryState: story.CurrentStoryState{
			World:         []story.WorldFact{{ID: "world", Description: "CURRENT_FACT"}},
			Characters:    []story.CharacterState{{ID: "a", Name: "阿禾", Facts: []story.StateItem{{Value: "村民"}}, KnowledgeAndBeliefs: []story.StateItem{}, CommitmentsAndIntentions: []story.StateItem{}}},
			Relationships: []story.RelationshipState{},
		},
		StorySpine:       []string{"临时相处逐渐成为能在重要决定中相互依靠的关系，SPINE_FUTURE_SECRET"},
		CurrentDirection: testDirection(),
	}
}

func testProject() story.Project {
	return story.Project{Name: "test", Idea: "  RAW_USER_IDEA_ONLY\n\n", LengthProfile: "medium", MaxCalls: 100}
}

func accepted() story.EditorDecision {
	return story.EditorDecision{
		ChapterDecision: story.EditorAccept,
		Assessment: story.EditorAssessment{
			Contribution: "本章形成了新的关系体验", Sequence: "承接前章且没有重复旧功能", Execution: "人物行为和场景成立",
		},
		BlockingIssues: []string{}, DirectionReview: story.DirectionReviewRequest{},
	}
}

func extracted() story.CommitResult {
	return story.CommitResult{
		StatePatch: story.StatePatch{
			World:         story.CollectionPatch[story.WorldFact]{Upsert: []story.WorldFact{}, Remove: []string{}},
			Characters:    story.CollectionPatch[story.CharacterPatch]{Upsert: []story.CharacterPatch{}, Remove: []string{}},
			Relationships: story.CollectionPatch[story.RelationshipState]{Upsert: []story.RelationshipState{}, Remove: []string{}},
		},
		TrajectoryEntry: story.TrajectoryMove{StoryMove: "TRAJECTORY_FOR_WRITER", NarrativeShape: "相处 → 感受陪伴"},
		ChapterSummary:  "LEDGER_NOT_FOR_WRITER",
	}
}

func newFake(chapters int) *scriptedGenerator {
	fake := &scriptedGenerator{
		responses: map[string]string{"architect": mustJSON(testGenesis())}, failures: map[string]error{}, requests: map[string]llm.Request{},
	}
	for number := 1; number <= chapters; number++ {
		fake.responses[chapterStage(number, "write")] = fmt.Sprintf("# 第%d章 相处\n\n阿禾和朋友一起做饭。", number)
		fake.responses[chapterStage(number, "editor", 1)] = mustJSON(accepted())
		fake.responses[chapterStage(number, "commit")] = mustJSON(extracted())
		if number%directionReviewInterval == 0 {
			fake.responses[chapterStage(number, "director")] = mustJSON(story.DirectorDecision{
				Action: story.DirectorKeep, Direction: testDirection(), Reason: "阶段变化仍在自然累积",
			})
		}
	}
	return fake
}

func initializeTest(t *testing.T, fake *scriptedGenerator) (*Engine, *store.Store) {
	t.Helper()
	files := store.New(filepath.Join(t.TempDir(), "novel"))
	engine, err := New(fake, files, testProject().MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Initialize(context.Background(), testProject()); err != nil {
		t.Fatal(err)
	}
	return engine, files
}

func TestRollingFlowGivesWriterLocalPlanningContextAndStopsAfterAcceptedCompletion(t *testing.T) {
	// 场景：七章均被接受，第三、六章提交后正常复查 Direction，最后一章由 Editor 确认完结。
	// 预期：Writer 获得局部规划所需的 Direction/Trajectory，但仍看不到 User Idea 与全书 Ledger。
	fake := newFake(7)
	complete := accepted()
	complete.StoryComplete = true
	fake.responses[chapterStage(7, "editor", 1)] = mustJSON(complete)
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	state, err := files.LoadState()
	if err != nil || state.Chapter != 7 || !state.Completed || state.DirectionReviewedAfterChapter != 6 {
		t.Fatalf("未正确完结或复查 Direction: %#v %v", state, err)
	}
	if len(state.RecentTrajectory) != 5 || state.RecentTrajectory[0].Chapter != 3 {
		t.Fatalf("轨迹未滚动: %#v", state.RecentTrajectory)
	}
	ledger, err := files.LoadLedger()
	if err != nil || len(ledger) != 7 {
		t.Fatalf("总账不是全部正式章节: %#v %v", ledger, err)
	}

	for stage, request := range fake.requests {
		if request.Role == llm.RoleWriter {
			for _, forbidden := range []string{"RAW_USER_IDEA_ONLY", "LEDGER_NOT_FOR_WRITER", "SPINE_FUTURE_SECRET", "story_spine", "user_idea", "chapter_ledger", "written_characters"} {
				if strings.Contains(request.Input, forbidden) {
					t.Fatalf("%s 泄露 %s", stage, forbidden)
				}
			}
			for _, required := range []string{"DIRECTION_FOR_WRITER", "current_direction", "recent_trajectory"} {
				if !strings.Contains(request.Input, required) {
					t.Fatalf("%s 缺少局部规划依据 %s", stage, required)
				}
			}
			var input map[string]any
			if err := json.Unmarshal([]byte(request.Input), &input); err != nil || len(input) != 7 {
				t.Fatalf("Writer 输入白名单错误: %v %v", input, err)
			}
		}
		if request.Role == llm.RoleEditor {
			if strings.Contains(request.Input, "RAW_USER_IDEA_ONLY") || strings.Contains(request.Input, "SPINE_FUTURE_SECRET") || !strings.Contains(request.Input, "\"chapter_ledger\"") {
				t.Fatalf("Editor 权限错误: %s", request.Input)
			}
		}
		if request.Role == llm.RoleCommit {
			var input map[string]any
			_ = json.Unmarshal([]byte(request.Input), &input)
			if len(input) != 5 || input["accepted_chapter"] == nil || input["previous_current_story_state"] == nil {
				t.Fatalf("Commit 输入越权: %v", input)
			}
			// Commit 使用章初规划和已提交的五章轨迹，不能提前包含本章尚未提取的结果。
			var received commitInput
			if err := json.Unmarshal([]byte(request.Input), &received); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(received.StorySpine, testGenesis().StorySpine) || !reflect.DeepEqual(received.CurrentDirection, testDirection()) {
				t.Fatalf("%s 未收到当前 Spine/Direction", stage)
			}
			var number int
			if _, err := fmt.Sscanf(stage, "chapter_%d_commit", &number); err != nil {
				t.Fatal(err)
			}
			if len(received.RecentTrajectory) != min(number-1, 5) {
				t.Fatalf("%s 轨迹窗口错误", stage)
			}
			for index, entry := range received.RecentTrajectory {
				if entry.Chapter != max(1, number-5)+index || entry.StoryMove != extracted().TrajectoryEntry.StoryMove {
					t.Fatalf("%s 未使用此前正式轨迹: %#v", stage, entry)
				}
			}
		}
	}

	for _, role := range []string{llm.RoleArchitect, llm.RoleDirector, llm.RoleWriter, llm.RoleEditor, llm.RoleCommit} {
		seen := false
		for _, request := range fake.requests {
			seen = seen || request.Role == role
		}
		if !seen {
			t.Fatalf("角色未执行: %s", role)
		}
	}

	// 已完成故事再次运行不应再调用 Writer 或 Director。
	calls := len(fake.order)
	if err := engine.Run(context.Background(), 0); err != nil || len(fake.order) != calls {
		t.Fatal("完结后仍然调用模型")
	}
}

func TestDirectorRunsAfterThirdCommitAndUpdatesNextWriter(t *testing.T) {
	// 场景：前三章使用初始 Direction；第三章提交后 Director 认为阶段需要调整，然后继续生成第四章。
	// 预期：调用顺序严格为 Commit3 → Director3 → Writer4，第四章 Writer 直接读取新 Direction。
	fake := newFake(4)
	updated := story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "UPDATED_STAGE", DesiredShift: "共同承担外部后果", ReaderExpectation: "读者等待两人如何共同作出代价更高的选择"}
	fake.responses[chapterStage(3, "director")] = mustJSON(story.DirectorDecision{
		Action: story.DirectorAdjust, Direction: updated, Reason: "信任已经形成，阶段重心需要转向共同承担",
	})
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 4); err != nil {
		t.Fatal(err)
	}

	wantOrder := []string{
		"architect",
		"chapter_001_write", "chapter_001_editor_1", "chapter_001_commit",
		"chapter_002_write", "chapter_002_editor_1", "chapter_002_commit",
		"chapter_003_write", "chapter_003_editor_1", "chapter_003_commit", "chapter_003_director",
		"chapter_004_write", "chapter_004_editor_1", "chapter_004_commit",
	}
	if !reflect.DeepEqual(fake.order, wantOrder) {
		t.Fatalf("Director 接线顺序错误: %v", fake.order)
	}
	if !strings.Contains(fake.requests["chapter_004_write"].Input, "UPDATED_STAGE") {
		t.Fatal("下一章 Writer 没有读到 Director 更新后的阶段方向")
	}
	if !strings.Contains(fake.requests["chapter_003_director"].Input, "\"chapter\": 3") || !strings.Contains(fake.requests["chapter_003_director"].Input, "LEDGER_NOT_FOR_WRITER") {
		t.Fatal("Director 没有读取第三章提交后的最新 Trajectory 与 Ledger")
	}
	state, err := files.LoadState()
	if err != nil || state.Direction != updated || state.DirectionVersion != 2 || state.DirectionReviewedAfterChapter != 3 {
		t.Fatalf("Director 结论没有正式生效: %#v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(files.Root(), "direction-reviews", "003.json")); err != nil {
		t.Fatalf("Direction Review 没有审计记录: %v", err)
	}
	commitData, _ := os.ReadFile(filepath.Join(files.Root(), "commits", "003.json"))
	if strings.Contains(string(commitData), "plan") || strings.Contains(string(commitData), "current_direction") {
		t.Fatal("章节 Commit 仍然拥有 Planner 或 Direction")
	}
}

func TestEditorCanRequestDirectionReviewBeforeScheduledChapter(t *testing.T) {
	// 场景：第一章本身成立，但 Editor 发现序列层面的方向问题并请求提前复查。
	// 预期：第一章先提交，Director 随后收到原因；章节接收与阶段复查互不冲突。
	fake := newFake(1)
	review := accepted()
	review.DirectionReview = story.DirectionReviewRequest{Requested: true, Reason: "近期叙事功能开始重复"}
	fake.responses["chapter_001_editor_1"] = mustJSON(review)
	fake.responses["chapter_001_director"] = mustJSON(story.DirectorDecision{
		Action: story.DirectorKeep, Direction: testDirection(), Reason: "当前只有一章证据，阶段方向仍可继续",
	})
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.order, []string{"architect", "chapter_001_write", "chapter_001_editor_1", "chapter_001_commit", "chapter_001_director"}) {
		t.Fatalf("提前复查顺序错误: %v", fake.order)
	}
	if !strings.Contains(fake.requests["chapter_001_director"].Input, "近期叙事功能开始重复") {
		t.Fatal("Editor Escalation 没有交给 Director")
	}
	state, _ := files.LoadState()
	if state.DirectionReviewedAfterChapter != 1 || state.DirectionVersion != 1 {
		t.Fatalf("KEEP 的复查元数据错误: %#v", state)
	}
}

func TestDirectorFailureAfterCommitRetriesBeforeNextWriter(t *testing.T) {
	// 场景：第三章已完整提交，但紧随其后的 Director 调用失败。
	// 预期：HEAD 保持第三章；新进程先重试 Director，成功后才允许 Writer 写第四章。
	fake := newFake(3)
	fake.failures["chapter_003_director"] = errors.New("阶段判断失败")
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 3); err == nil {
		t.Fatal("Director 故障没有停止运行")
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 3 || state.DirectionReviewedAfterChapter != 0 {
		t.Fatalf("Director 故障破坏了已提交章节或伪造复查成功: %#v %v", state, err)
	}

	retry := newFake(4)
	delete(retry.responses, "architect")
	resumed, err := New(retry, files, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"chapter_003_director", "chapter_004_write", "chapter_004_editor_1", "chapter_004_commit"}
	if !reflect.DeepEqual(retry.order, want) {
		t.Fatalf("Director 恢复顺序错误: %v", retry.order)
	}
}

func TestWriterRevisionKeepsRoleBoundariesAndOnlyAcceptedDraftBecomesHistory(t *testing.T) {
	// 场景：Writer 初稿存在实现问题，Editor 退回后修订稿通过。
	// 预期：没有 Planner 回退；修订只获得同一 Writer 上下文和阻断问题，Commit 只读取最终正文。
	fake := newFake(1)
	issues := []string{"让人物的选择有可信依据", "不要重复上一章已经形成的结果"}
	fake.responses["chapter_001_write"] = "# 第1章 初稿\n\nREJECTED_DRAFT"
	fake.responses["chapter_001_editor_1"] = mustJSON(story.EditorDecision{
		ChapterDecision: story.EditorRevise,
		Assessment:      story.EditorAssessment{Contribution: "局部选择没有形成新贡献", Sequence: "重复前章结果", Execution: "人物动机不足"},
		BlockingIssues:  issues, DirectionReview: story.DirectionReviewRequest{},
	})
	fake.responses["chapter_001_revise_1"] = "# 第1章 修订稿\n\n人物经过犹豫后承担了选择。"
	fake.responses["chapter_001_editor_2"] = mustJSON(accepted())
	engine, _ := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"architect", "chapter_001_write", "chapter_001_editor_1", "chapter_001_revise_1", "chapter_001_editor_2", "chapter_001_commit"}
	if !reflect.DeepEqual(fake.order, want) {
		t.Fatalf("修订顺序错误: %v", fake.order)
	}
	if strings.Contains(fake.requests["chapter_001_commit"].Input, "REJECTED_DRAFT") {
		t.Fatal("否决稿件进入事实提取")
	}
	var revision struct {
		Context        writerContext `json:"context"`
		BlockingIssues []string      `json:"blocking_issues"`
	}
	if err := json.Unmarshal([]byte(fake.requests["chapter_001_revise_1"].Input), &revision); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(revision.BlockingIssues, issues) || revision.Context.CurrentDirection != testDirection() {
		t.Fatalf("修订上下文错误: %#v", revision)
	}
	if strings.Contains(fake.requests["chapter_001_revise_1"].Input, "RAW_USER_IDEA_ONLY") || strings.Contains(fake.requests["chapter_001_revise_1"].Input, "chapter_ledger") {
		t.Fatal("修订阶段泄露 User Idea 或 Ledger")
	}
}

func TestCommitFailureCanResumeFromUnchangedCheckpoint(t *testing.T) {
	// 场景：Editor 接受了正文，但事实提取失败；随后以新引擎从磁盘重试。
	// 预期：失败时 HEAD/Core/Direction 都不变，重试从同一章重新生成。
	fake := newFake(1)
	fake.failures["chapter_001_commit"] = errors.New("提取失败")
	engine, files := initializeTest(t, fake)
	before, _ := os.ReadFile(filepath.Join(files.Root(), "story-core.json"))
	if err := engine.Run(context.Background(), 1); err == nil {
		t.Fatal("提取失败却通过")
	}
	state, _ := files.LoadState()
	if state.Chapter != 0 || state.WrittenCharacters != 0 || state.Completed {
		t.Fatal("提取失败推进了状态")
	}
	delete(fake.failures, "chapter_001_commit")
	resumed, err := New(fake, files, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	state, _ = files.LoadState()
	after, _ := os.ReadFile(filepath.Join(files.Root(), "story-core.json"))
	if state.Chapter != 1 || string(before) != string(after) {
		t.Fatal("恢复没有保持正式历史和 Core")
	}
}

func TestWorkflowPersistsResolvedAtomicCharacterPatch(t *testing.T) {
	// 场景：Architect 与 Commit 都让程序生成新人物状态条目的稳定 ID。
	// 预期：.work 保留模型空 ID，正式 commit/checkpoint 使用同一可重放 ID，旧 facts 不丢失。
	fake := newFake(1)
	result := extracted()
	result.StatePatch.Characters.Upsert = []story.CharacterPatch{{
		ID: "a", Name: "阿禾",
		Facts:                    story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{}, Remove: []string{}},
		KnowledgeAndBeliefs:      story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{{Value: "知道道路已经封闭"}}, Remove: []string{}},
		CommitmentsAndIntentions: story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{}, Remove: []string{}},
	}}
	fake.responses["chapter_001_commit"] = mustJSON(result)
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	var raw story.CommitResult
	rawData, _ := os.ReadFile(filepath.Join(files.Root(), ".work", "001-chapter_001_commit.json"))
	if err := json.Unmarshal(rawData, &raw); err != nil || raw.StatePatch.Characters.Upsert[0].KnowledgeAndBeliefs.Upsert[0].ID != "" {
		t.Fatal(".work 没有保留模型原始空 ID")
	}
	var committed story.ChapterCommit
	commitData, _ := os.ReadFile(filepath.Join(files.Root(), "commits", "001.json"))
	if err := json.Unmarshal(commitData, &committed); err != nil {
		t.Fatal(err)
	}
	itemID := committed.Result.StatePatch.Characters.Upsert[0].KnowledgeAndBeliefs.Upsert[0].ID
	current, _ := files.LoadState()
	character := current.Story.Characters[0]
	if itemID == "" || len(character.Facts) != 1 || len(character.KnowledgeAndBeliefs) != 1 || character.KnowledgeAndBeliefs[0].ID != itemID {
		t.Fatalf("原子 Patch 未正确提交: %#v", character)
	}
}

func TestRevisionBudgetStopsWithoutAutoAccept(t *testing.T) {
	// 场景：Editor 持续要求 Writer 修订，项目调用预算不足以完成下一轮。
	// 预期：预算只能停止，不能自动接受最后一稿。
	fake := newFake(1)
	revise := story.EditorDecision{
		ChapterDecision: story.EditorRevise,
		Assessment:      story.EditorAssessment{Contribution: "没有形成贡献", Sequence: "重复", Execution: "实现不足"},
		BlockingIssues:  []string{"重新选择自然的局部发展"}, DirectionReview: story.DirectionReviewRequest{},
	}
	fake.responses["chapter_001_editor_1"] = mustJSON(revise)
	fake.responses["chapter_001_revise_1"] = "# 第1章 二稿\n\n仍未成立。"
	fake.responses["chapter_001_editor_2"] = mustJSON(revise)
	engine, files := initializeTest(t, fake)
	engine.maxCalls = 5 // Architect 已使用一次，Write/Editor/Revise/Editor 用完剩余额度。
	if err := engine.Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "模型调用上限") {
		t.Fatalf("预算没有停止循环: %v", err)
	}
	state, _ := files.LoadState()
	if state.Chapter != 0 {
		t.Fatal("预算耗尽后自动接受")
	}
}

func TestOutputTypesRejectUnauthorizedFields(t *testing.T) {
	// 场景：低权限阶段试图输出 Direction、状态补丁或已经删除的 Planner 动作。
	// 预期：严格解码拒绝越权字段。
	if _, err := decodeStructured[story.CommitResult](`{"current_direction":{"focus":"越权"}}`); err == nil {
		t.Fatal("Commit 允许规划字段")
	}
	if _, err := decodeStructured[story.EditorDecision](`{"state_patch":{}}`); err == nil {
		t.Fatal("Editor 允许事实补丁")
	}
	decision, err := decodeStructured[story.EditorDecision](`{"chapter_decision":"RETURN_TO_PLANNER"}`)
	if err == nil && story.ValidateEditorDecision(decision) == nil {
		t.Fatal("Editor 仍允许退回已删除的 Planner")
	}
}

func TestMissingCommitPatchIsNotSilentlyTreatedAsEmpty(t *testing.T) {
	fake := newFake(1)
	fake.responses["chapter_001_commit"] = `{"trajectory_entry":{"story_move":"安静相处","narrative_shape":"做饭"},"chapter_summary":"一起吃饭"}`
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "state_patch") {
		t.Fatalf("漏字段没有失败: %v", err)
	}
	state, _ := files.LoadState()
	if state.Chapter != 0 {
		t.Fatal("不完整提取推进了 HEAD")
	}
}

func TestMalformedWriterDraftIsSavedBeforeTechnicalRejection(t *testing.T) {
	fake := newFake(1)
	fake.responses["chapter_001_write"] = "这是没有标题的原始输出。"
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err == nil {
		t.Fatal("错误正文通过了技术检查")
	}
	content, err := os.ReadFile(filepath.Join(files.Root(), ".work", "001-chapter_001_draft_1.md"))
	if err != nil || !strings.Contains(string(content), "这是没有标题的原始输出") {
		t.Fatalf("坏正文没有保留: %q %v", content, err)
	}
	if _, ok := fake.requests["chapter_001_editor_1"]; ok {
		t.Fatal("格式无效后仍调用 Editor")
	}
}
