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
func testGenesis() story.Genesis {
	return story.Genesis{
		Title: "测试故事", StoryCore: story.StoryCore{
			StoryEngine:        story.StoryEngine{Loop: "人物回应事件，回应改变处境", ProgressionAxis: "逐渐承担选择"},
			ReaderPromises:     []story.ReaderPromise{{Promise: "信任", PayoffShape: "共同经历"}, {Promise: "责任", PayoffShape: "选择的结果"}, {Promise: "旅程", PayoffShape: "找到自己的落点"}},
			ExperienceContract: story.ExperienceContract{TargetExperience: "普通人的生活", NarrativePrinciples: []string{"通过场景呈现"}, DriftBoundaries: []string{"连续解释"}},
		},
		InitialStoryState: story.CurrentStoryState{World: []story.WorldFact{{ID: "world", Description: "CURRENT_FACT"}}, Characters: []story.CharacterState{{ID: "a", Name: "阿禾", Facts: []string{"村民"}, KnowledgeAndBeliefs: []string{}, CommitmentsAndIntentions: []string{}}}, Relationships: []story.RelationshipState{}},
		CurrentDirection:  story.Direction{Focus: "DIRECTION_NOT_FOR_WRITER", DesiredShift: "自然建立信任"},
	}
}
func testProject() story.Project {
	return story.Project{Name: "test", Idea: "  RAW_USER_IDEA_ONLY\n\n", LengthProfile: "medium", MaxCalls: 100}
}
func testPlan() story.ChapterPlan {
	return story.ChapterPlan{DirectionAction: "KEEP", ChapterIntent: story.ChapterIntent{IntendedEffect: "INTENT_NOT_FOR_COMMIT", WhyNow: "承接已有故事", Constraints: []string{}}}
}
func accepted() story.EditorDecision {
	return story.EditorDecision{Action: story.EditorAccept, Reason: "正文效果成立", BlockingIssues: []string{}}
}
func extracted() story.CommitResult {
	return story.CommitResult{
		StatePatch:      story.StatePatch{World: story.CollectionPatch[story.WorldFact]{Upsert: []story.WorldFact{}, Remove: []string{}}, Characters: story.CollectionPatch[story.CharacterState]{Upsert: []story.CharacterState{}, Remove: []string{}}, Relationships: story.CollectionPatch[story.RelationshipState]{Upsert: []story.RelationshipState{}, Remove: []string{}}},
		TrajectoryEntry: story.TrajectoryMove{StoryMove: "TRAJECTORY_NOT_FOR_WRITER", NarrativeShape: "相处 → 感受陪伴"}, ChapterSummary: "LEDGER_NOT_FOR_WRITER",
	}
}
func newFake(chapters int) *scriptedGenerator {
	fake := &scriptedGenerator{responses: map[string]string{"architect": mustJSON(testGenesis())}, failures: map[string]error{}, requests: map[string]llm.Request{}}
	for n := 1; n <= chapters; n++ {
		fake.responses[chapterStage(n, "plan", 1)] = mustJSON(testPlan())
		fake.responses[chapterStage(n, "write", 1)] = fmt.Sprintf("# 第%d章 相处\n\n阿禾和朋友一起做饭。", n)
		fake.responses[chapterStage(n, "editor", 1, 1)] = mustJSON(accepted())
		fake.responses[chapterStage(n, "commit")] = mustJSON(extracted())
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

func TestRollingFlowKeepsWriterIsolatedAndStopsOnlyAfterAcceptedCompletion(t *testing.T) {
	// 场景：七章均被接受，最后一章由 Editor 明确确认完结；所有章节均允许空补丁。
	// 逐次检查实际生成请求，而非仅检查 Go 字段定义，覆盖路由、历史窗口、字数与停止时点。
	fake := newFake(7)
	complete := accepted()
	complete.StoryComplete = true
	fake.responses[chapterStage(7, "editor", 1, 1)] = mustJSON(complete)
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 7 || !state.Completed {
		t.Fatalf("未正确完结: %#v %v", state, err)
	}
	if len(state.RecentTrajectory) != 5 || state.RecentTrajectory[0].Chapter != 3 {
		t.Fatalf("轨迹未滚动: %#v", state.RecentTrajectory)
	}
	ledger, err := files.LoadLedger()
	if err != nil || len(ledger) != 7 {
		t.Fatalf("总账不是全部正式章节: %#v %v", ledger, err)
	}
	project, _ := files.LoadProject()
	if project.Idea != testProject().Idea {
		t.Fatal("User Idea 没有原样持久化")
	}

	for stage, request := range fake.requests {
		if request.Role == llm.RoleWriter {
			for _, forbidden := range []string{"RAW_USER_IDEA_ONLY", "DIRECTION_NOT_FOR_WRITER", "TRAJECTORY_NOT_FOR_WRITER", "LEDGER_NOT_FOR_WRITER", "user_idea", "current_direction", "recent_trajectory", "chapter_ledger", "relevant_context", "written_characters"} {
				if strings.Contains(request.Input, forbidden) {
					t.Fatalf("%s 泄露 %s", stage, forbidden)
				}
			}
			var input map[string]any
			if err := json.Unmarshal([]byte(request.Input), &input); err != nil {
				t.Fatal(err)
			}
			if len(input) != 6 {
				t.Fatalf("Writer 超出六项输入白名单: %v", input)
			}
			if !strings.Contains(request.Input, "CURRENT_FACT") || !strings.Contains(request.Input, "INTENT_NOT_FOR_COMMIT") {
				t.Fatal("Writer 缺少必要事实或意图")
			}
		}
		if request.Role == llm.RoleCommit {
			var input map[string]any
			_ = json.Unmarshal([]byte(request.Input), &input)
			if len(input) != 2 || input["accepted_chapter"] == nil || input["previous_current_story_state"] == nil {
				t.Fatalf("提取器输入越权: %v", input)
			}
			if strings.Contains(request.Input, "RAW_USER_IDEA_ONLY") || strings.Contains(request.Input, "INTENT_NOT_FOR_COMMIT") {
				t.Fatal("提取器混入创作授权或规划")
			}
		}
	}
	var planning plannerInput
	if err := json.Unmarshal([]byte(fake.requests[chapterStage(7, "plan", 1)].Input), &planning); err != nil {
		t.Fatal(err)
	}
	if len(planning.ChapterLedger) != 6 || len(planning.RecentTrajectory) != 5 || planning.LengthProgress.WrittenCharacters <= 0 || planning.UserIdea != testProject().Idea {
		t.Fatalf("Planner 缺少全局依据: %#v", planning)
	}
	if !strings.Contains(fake.requests[chapterStage(7, "editor", 1, 1)].Input, "LEDGER_NOT_FOR_WRITER") {
		t.Fatal("Editor 无法核对全书承诺")
	}
	for _, role := range []string{llm.RoleArchitect, llm.RolePlanner, llm.RoleWriter, llm.RoleEditor, llm.RoleCommit} {
		seen := false
		for _, request := range fake.requests {
			if request.Role == role {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("角色未执行: %s", role)
		}
	}
	// 已完成故事再次运行不应再调用 Planner 或 Writer。
	calls := len(fake.order)
	if err := engine.Run(context.Background(), 0); err != nil || len(fake.order) != calls {
		t.Fatal("完结后仍然调用了模型")
	}
}

func TestRevisionLimitReturnsToPlannerAndOnlyFinalDraftBecomesHistory(t *testing.T) {
	// 场景：Planner 首轮更新方向，三份稿件都未通过。第二轮 KEEP 该候选方向后成功。
	// 预期：两次修订后重新规划；不会自动接受第三稿；方向只随最终章提交，失败正文不记账。
	fake := newFake(1)
	direction := story.Direction{Focus: "UPDATED_DIRECTION", DesiredShift: "面对后果"}
	plan := testPlan()
	plan.DirectionAction = "UPDATE"
	plan.CurrentDirection = &direction
	fake.responses[chapterStage(1, "plan", 1)] = mustJSON(plan)
	revise := story.EditorDecision{Action: story.EditorRevise, Reason: "效果尚未成立", BlockingIssues: []string{"让人物的选择有可信依据"}}
	for draft := 1; draft <= 3; draft++ {
		fake.responses[chapterStage(1, "editor", 1, draft)] = mustJSON(revise)
	}
	fake.responses[chapterStage(1, "revise", 1, 1)] = "# 第1章 二稿\n\nREJECTED_DRAFT_TWO"
	fake.responses[chapterStage(1, "revise", 1, 2)] = "# 第1章 三稿\n\nREJECTED_DRAFT_THREE"
	fake.responses[chapterStage(1, "plan", 2)] = mustJSON(testPlan())
	fake.responses[chapterStage(1, "write", 2)] = "# 第1章 最终稿\n\n只有这一稿是真实历史。"
	fake.responses[chapterStage(1, "editor", 2, 1)] = mustJSON(accepted())
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"architect", "chapter_001_plan_1", "chapter_001_write_1", "chapter_001_editor_1_1", "chapter_001_revise_1_1", "chapter_001_editor_1_2", "chapter_001_revise_1_2", "chapter_001_editor_1_3", "chapter_001_plan_2", "chapter_001_write_2", "chapter_001_editor_2_1", "chapter_001_commit"}
	if !reflect.DeepEqual(fake.order, want) {
		t.Fatalf("修订与回退顺序错误: %v", fake.order)
	}
	state, _ := files.LoadState()
	if state.Direction != direction || state.Completed {
		t.Fatalf("方向或状态错误: %#v", state)
	}
	if strings.Contains(fake.requests["chapter_001_commit"].Input, "REJECTED_DRAFT") {
		t.Fatal("否决稿件进入事实提取")
	}
	if !strings.Contains(fake.requests["chapter_001_plan_2"].Input, "被退回的章节意图：INTENT_NOT_FOR_COMMIT") {
		t.Fatal("Planner 不知道原意图是什么")
	}
	if !strings.Contains(fake.requests["chapter_001_plan_2"].Input, "已经修订两次") {
		t.Fatal("重规划没有收到失败原因")
	}
	for _, stage := range []string{"chapter_001_revise_1_1", "chapter_001_revise_1_2"} {
		input := fake.requests[stage].Input
		if strings.Contains(input, "RAW_USER_IDEA_ONLY") || strings.Contains(input, "UPDATED_DIRECTION") || strings.Contains(input, "chapter_ledger") {
			t.Fatal("修订阶段泄露全局上下文")
		}
	}
}

func TestReturnToPlannerDoesNotForceWriterRevision(t *testing.T) {
	// 场景：第一份草稿暴露的是意图本身的问题。
	// 预期：直接交回 Planner，不在错误意图上消耗两次修订，也不重复调用 Architect。
	fake := newFake(1)
	fake.responses[chapterStage(1, "editor", 1, 1)] = mustJSON(story.EditorDecision{Action: story.EditorReplan, Reason: "意图已经完成", BlockingIssues: []string{"重新选择本章值得产生的效果"}})
	fake.responses[chapterStage(1, "plan", 2)] = mustJSON(testPlan())
	fake.responses[chapterStage(1, "write", 2)] = "# 第1章 新意图\n\n一个自然的后续。"
	fake.responses[chapterStage(1, "editor", 2, 1)] = mustJSON(accepted())
	engine, _ := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	for _, stage := range fake.order {
		if strings.Contains(stage, "revise") {
			t.Fatal("规划错误仍送给 Writer 修订")
		}
	}
}

func TestCommitFailureCanResumeFromUnchangedCheckpoint(t *testing.T) {
	// 场景：Editor 接受了正文，但事实提取调用失败；随后以新引擎从磁盘重试。
	// 预期：失败时 HEAD/Core/方向都不变，重试无需重新初始化，也不会跳过未提交章节。
	fake := newFake(1)
	fake.failures["chapter_001_commit"] = errors.New("提取失败")
	engine, files := initializeTest(t, fake)
	before, _ := os.ReadFile(filepath.Join(files.Root(), "story-core.json"))

	// 失败时保留第 0 章状态；ACCEPT 不能提前计入字数、推进章号或标记完结。
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

func TestReplanningBudgetStopsWithoutAutoAccept(t *testing.T) {
	// 场景：模型持续拒绝同一章；项目预算只剩下一个完整的规划/写作/审核循环。
	// 预期：下一次 Planner 调用前终止，不能以预算耗尽作为接受正文的理由。
	fake := newFake(1)
	fake.responses["chapter_001_editor_1_1"] = mustJSON(story.EditorDecision{Action: story.EditorReplan, Reason: "意图不成立", BlockingIssues: []string{"重新考虑"}})
	engine, files := initializeTest(t, fake)
	engine.maxCalls = 4
	if err := engine.Run(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "模型调用上限") {
		t.Fatalf("预算没有停止循环: %v", err)
	}
	state, _ := files.LoadState()
	if state.Chapter != 0 {
		t.Fatal("预算耗尽后自动接受")
	}
	if _, exists := fake.requests["chapter_001_commit"]; exists {
		t.Fatal("未接受正文被记账")
	}
}

func TestOutputTypesRejectUnauthorizedFields(t *testing.T) {
	// 场景：低权限阶段试图输出方向或完成标记，或者 Editor 夹带状态补丁。
	// 预期：严格解码直接识别越界字段，不能静默接受后写入正式状态。
	if _, err := decodeStructured[story.CommitResult](`{"current_direction":{"focus":"越权"}}`); err == nil {
		t.Fatal("Commit 允许规划字段")
	}
	if _, err := decodeStructured[story.EditorDecision](`{"state_patch":{}}`); err == nil {
		t.Fatal("Editor 允许事实补丁")
	}
	if _, err := decodeStructured[story.ChapterPlan](`{"story_core":{}}`); err == nil {
		t.Fatal("Planner 允许改写 Core")
	}
}

func TestMissingCommitPatchIsNotSilentlyTreatedAsEmpty(t *testing.T) {
	// 场景：提取器给出摘要与轨迹，但漏掉整个 state_patch。Go 默认解码会得到零值。
	// 预期：缺失字段不是“没有事实变化”，必须保存失败原文并停止，不能补造一个空补丁。
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
	if _, ok := fake.requests["chapter_001_commit_format_repair"]; ok {
		t.Fatal("缺失事实被交给格式修复器编造")
	}
}

func TestMalformedWriterDraftIsSavedBeforeTechnicalRejection(t *testing.T) {
	// 场景：Writer 没有返回章节标题，无法进入 Editor。
	// 预期：不推进 HEAD、不调用 Editor，同时保留实际坏正文，便于定位提示词或模型问题。
	fake := newFake(1)
	fake.responses["chapter_001_write_1"] = "这是没有标题的原始输出。"
	engine, files := initializeTest(t, fake)
	if err := engine.Run(context.Background(), 1); err == nil {
		t.Fatal("错误正文通过了技术检查")
	}
	content, err := os.ReadFile(filepath.Join(files.Root(), ".work", "001-chapter_001_draft_1_1.md"))
	if err != nil || !strings.Contains(string(content), "这是没有标题的原始输出") {
		t.Fatalf("坏正文没有保留: %q %v", content, err)
	}
	if _, ok := fake.requests["chapter_001_editor_1_1"]; ok {
		t.Fatal("格式无效后仍调用 Editor")
	}
	state, _ := files.LoadState()
	if state.Chapter != 0 {
		t.Fatal("坏正文推进了 HEAD")
	}
}
