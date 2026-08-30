package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

// fakeGenerator 按阶段返回预设响应并统计调用次数，模拟模型而不产生网络费用。
// responses 的 key 直接使用 Engine 的 stage 名称，因此测试还能验证阶段编排是否正确。
type fakeGenerator struct {
	responses map[string][]string
	calls     map[string]int
}

func (fake *fakeGenerator) Generate(_ context.Context, request llm.Request) (llm.Result, error) {
	// 该 fake 按真实 Generator 契约消费阶段响应，超出预设即失败，能及时暴露意外重试或循环。
	// 定位当前阶段已经消费到的响应位置。
	index := fake.calls[request.Stage]
	values := fake.responses[request.Stage]

	// 拒绝测试未预设的额外调用，保护“最多一次修复”的业务规则。
	if index >= len(values) {
		return llm.Result{}, fmt.Errorf("unexpected call: %s #%d", request.Stage, index+1)
	}

	// 推进调用计数并返回固定的可审计模型结果。
	fake.calls[request.Stage]++
	return llm.Result{
		Text: values[index], Model: "fake", ResponseID: fmt.Sprintf("%s-%d", request.Stage, index),
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}, Duration: time.Millisecond,
	}, nil
}

func TestEngineRunsOneCompleteChapterTransaction(t *testing.T) {
	// 场景：完整 fake 模型依次返回 Genesis、计划、正文、delta 和通过审核。
	// 预期：Initialize 建立第 0 章，Run(1) 原子提交第 1 章并产生 5 条成功调用记录，
	// 验证主链路阶段顺序和提交边界。

	// 准备临时项目、项目契约和成功路径 fake。
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
	files := store.New(root)
	engine, err := New(fake, files, project.MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 执行初始化，建立 Bible 和第 0 章。
	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}

	// 只运行一章并验证正式提交结果。
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	assertOneChapterCommitted(t, files, root, 5)
}

func TestEngineRepairsOneInvalidStateDelta(t *testing.T) {
	// 场景：书记员第一次引用不存在的事实，随后返回合法修复 delta。
	// 预期：只触发一次 record_repair，最终章节提交且用量增加 1，验证状态修复有界且可恢复。

	// 准备成功 fake，并把首次书记员响应替换成非法事实引用。
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
	invalid := testDelta()
	invalid.CharacterStates[0].Knowledge = []story.Knowledge{{FactID: "unknown", Belief: "knows", Confidence: 100}}
	fake.responses["chapter_001_record"] = []string{mustJSON(invalid)}
	fake.responses["chapter_001_record_repair"] = []string{mustJSON(testDelta())}

	// 初始化项目并运行一章，触发状态修复分支。
	files := store.New(root)
	engine, err := New(fake, files, project.MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// 确认修复只发生一次且章节仍能提交。
	if fake.calls["chapter_001_record_repair"] != 1 {
		t.Fatal("expected exactly one state repair call")
	}
	assertOneChapterCommitted(t, files, root, 6)
}

func TestEngineRepairsOneMalformedStructuredResponse(t *testing.T) {
	// 场景：计划阶段返回非 JSON 文本，format_repair 返回原计划的合法 JSON。
	// 预期：保存坏原文并只修复一次后继续完成章节，验证结构化输出容错不会无限重试。

	// 准备成功 fake，并把计划阶段首次响应替换成非法 JSON。
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
	validPlan := fake.responses["chapter_001_plan"][0]
	fake.responses["chapter_001_plan"] = []string{"这不是 JSON"}
	fake.responses["chapter_001_plan_format_repair"] = []string{validPlan}

	// 初始化并运行一章，触发 format_repair 分支。
	files := store.New(root)
	engine, err := New(fake, files, project.MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// 确认格式修复调用恰好一次且最终章节提交。
	if fake.calls["chapter_001_plan_format_repair"] != 1 {
		t.Fatal("expected exactly one format repair call")
	}
	assertOneChapterCommitted(t, files, root, 6)
}

func TestMergeReviewOnlyKeepsHardIssueRepairs(t *testing.T) {
	// 场景：编辑器同时给出硬伤和可选质量建议。
	// 预期：合并后只留下硬伤修订指令且保持不通过，验证硬门槛优先级不会被软建议稀释。

	// 准备同时包含硬伤和软建议的编辑器结果。
	review := story.Review{
		Passed: false,
		Score:  55,
		HardIssues: []story.Issue{{
			Code: "missing_payoff", Suggestion: "补写厂办代表的明确结论",
		}},
		QualityIssues:        []story.Issue{{Code: "weak_transition", Suggestion: "扩写申请过程"}},
		RevisionInstructions: []string{"补写厂办代表的明确结论", "扩写申请过程"},
	}

	// 合并确定性检查并验证只保留硬伤动作。
	merged := mergeDeterministicIssues(review, nil)
	if merged.Passed {
		t.Fatal("存在硬伤时不得通过")
	}
	if len(merged.RevisionInstructions) != 1 || merged.RevisionInstructions[0] != "补写厂办代表的明确结论" {
		t.Fatalf("修订指令应只保留硬伤修复，实际为 %#v", merged.RevisionInstructions)
	}
}

func testProject() story.Project {
	// 返回覆盖最小运行契约的项目夹具：三章、可测试字数窗口和有限调用预算。
	return story.Project{
		Version: story.FormatVersion, Name: "test", Idea: "工程师修复旧机床",
		Provider: "deepseek", Model: "deepseek-v4-flash", TargetChapters: 3,
		ChapterMinChars: 500, ChapterMaxChars: 800, MaxCalls: 10, CreatedAt: time.Unix(0, 0).UTC(),
	}
}

func newHappyFake(project story.Project) *fakeGenerator {
	// 组装一条成功路径的阶段响应；测试在此基础上替换某个阶段以注入故障。
	// 准备总导演和章节计划响应。
	genesis := testGenesis(project.TargetChapters)
	plan := story.ChapterPlan{
		Number: 1, Title: "旧机床", Purpose: "让主角主动争取第一次机会", ActiveThreads: []string{"comeback"},
		Scenes: []story.ScenePlan{{Order: 1}, {Order: 2}, {Order: 3}},
		Payoff: "修复成功", Hook: "发现异常声音", Forbidden: []string{"提前揭密"}, TargetChars: 600,
	}

	// 准备正文、状态增量和通过审核响应。
	delta := testDelta()
	review := story.Review{Passed: true, Score: 82, HardIssues: []story.Issue{}, QualityIssues: []story.Issue{}, RevisionInstructions: []string{}}

	// 按 Engine 使用的稳定阶段名组装 fake 响应表。
	return &fakeGenerator{
		responses: map[string][]string{
			"architect":          {mustJSON(genesis)},
			"chapter_001_plan":   {mustJSON(plan)},
			"chapter_001_write":  {"# 第1章 旧机床\n\n" + strings.Repeat("顾川检查机床并作出选择。", 50)},
			"chapter_001_record": {mustJSON(delta)},
			"chapter_001_review": {mustJSON(review)},
		},
		calls: map[string]int{},
	}
}

func testGenesis(target int) story.Genesis {
	// 构造完整 Bible 和第 0 章状态，包含三名人物、连续两段故事弧和一条主线。
	return story.Genesis{
		Bible: story.StoryBible{
			Title: "听见机器", Genre: "都市", Logline: "工程师翻身", ReaderPromise: "专业逆袭", Ending: "团队胜利",
			ProtagonistID: "hero", Style: story.StyleGuide{PointOfView: "第三人称", Tone: "克制", ProseRules: []string{"场景推进"}},
			Arcs: []story.StoryArc{
				{ID: "a1", Name: "起步", StartChapter: 1, EndChapter: 1, Goal: "获得机会", Climax: "修好机器"},
				{ID: "a2", Name: "翻身", StartChapter: 2, EndChapter: target, Goal: "完成翻身", Climax: "团队成功"},
			},
			Characters: []story.Character{{ID: "hero", Name: "顾川", Role: "主角"}, {ID: "ally", Name: "林桐", Role: "伙伴"}, {ID: "enemy", Name: "赵启", Role: "对手"}},
			CanonRules: []string{"能力只给线索"}, RecurringMotifs: []string{"机器声"},
		},
		InitialState: story.InitialState{
			Characters: []story.CharacterState{
				{CharacterID: "hero", Goal: "找到工作", Emotion: "压抑", Location: "厂门", Knowledge: []story.Knowledge{}, Relationships: []story.Relationship{}},
				{CharacterID: "ally", Goal: "保住车间", Emotion: "焦虑", Location: "车间", Knowledge: []story.Knowledge{}, Relationships: []story.Relationship{}},
				{CharacterID: "enemy", Goal: "封锁顾川", Emotion: "轻蔑", Location: "办公室", Knowledge: []story.Knowledge{}, Relationships: []story.Relationship{}},
			},
			Facts:            []story.Fact{},
			Threads:          []story.PlotThreadState{{ID: "comeback", Name: "翻身", Kind: "main", Status: "active", Progress: "尚未开始", PlannedPayoffChapter: target}},
			DirectorGuidance: []string{"先让主角获得小胜"},
		},
	}
}

func testDelta() story.StateDelta {
	// 构造第 1 章可通过领域校验的 delta，供正常路径及修复后的结果复用。
	return story.StateDelta{
		Chapter:         1,
		Summary:         story.ChapterSummary{Number: 1, Title: "旧机床", Summary: "顾川修好旧机床", KeyChanges: []string{"获得机会"}},
		NewFacts:        []story.Fact{{ID: "first_fix", Description: "顾川修好机床", Visibility: "public", SinceChapter: 1}},
		CharacterStates: []story.CharacterState{{CharacterID: "hero", Goal: "拿下订单", Emotion: "振奋", Location: "车间", Knowledge: []story.Knowledge{}, Relationships: []story.Relationship{}}},
		ThreadStates:    []story.PlotThreadState{{ID: "comeback", Name: "翻身", Kind: "main", Status: "active", Progress: "获得第一次机会", LastTouchedChapter: 1, PlannedPayoffChapter: 3}},
		NewThreads:      []story.PlotThreadState{},
		TimelineEvents:  []story.TimelineEvent{{Chapter: 1, Description: "修好旧机床", Participants: []string{"hero"}}},
	}
}

func assertOneChapterCommitted(t *testing.T, files *store.Store, root string, expectedUsage int) {
	// 验证一次成功运行的外部可见结果：HEAD/state/章节文件和成功调用数均已推进。
	t.Helper()

	// 读取 HEAD 对应状态，确认章节和事实已推进。
	state, err := files.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Chapter != 1 || len(state.Facts) != 1 {
		t.Fatalf("unexpected state: %#v", state)
	}

	// 确认正式章节文件已经写入。
	if _, err := os.Stat(filepath.Join(root, "chapters", "001.md")); err != nil {
		t.Fatal(err)
	}

	// 确认用量日志只记录预期的成功调用数。
	usage, err := files.CountUsage()
	if err != nil || usage != expectedUsage {
		t.Fatalf("unexpected usage count: %d, %v", usage, err)
	}
}

func mustJSON(value any) string {
	// 测试夹具序列化失败属于编程错误，直接 panic 比在每个 map 初始化处重复处理更清晰。
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
