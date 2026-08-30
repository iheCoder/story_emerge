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

type fakeGenerator struct {
	responses map[string][]string
	calls     map[string]int
}

func (fake *fakeGenerator) Generate(_ context.Context, request llm.Request) (llm.Result, error) {
	index := fake.calls[request.Stage]
	values := fake.responses[request.Stage]
	if index >= len(values) {
		return llm.Result{}, fmt.Errorf("unexpected call: %s #%d", request.Stage, index+1)
	}
	fake.calls[request.Stage]++
	return llm.Result{
		Text: values[index], Model: "fake", ResponseID: fmt.Sprintf("%s-%d", request.Stage, index),
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}, Duration: time.Millisecond,
	}, nil
}

func TestEngineRunsOneCompleteChapterTransaction(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
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
	assertOneChapterCommitted(t, files, root, 5)
}

func TestEngineRepairsOneInvalidStateDelta(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
	invalid := testDelta()
	invalid.CharacterStates[0].Knowledge = []story.Knowledge{{FactID: "unknown", Belief: "knows", Confidence: 100}}
	fake.responses["chapter_001_record"] = []string{mustJSON(invalid)}
	fake.responses["chapter_001_record_repair"] = []string{mustJSON(testDelta())}
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
	if fake.calls["chapter_001_record_repair"] != 1 {
		t.Fatal("expected exactly one state repair call")
	}
	assertOneChapterCommitted(t, files, root, 6)
}

func TestEngineRepairsOneMalformedStructuredResponse(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "novel")
	project := testProject()
	fake := newHappyFake(project)
	validPlan := fake.responses["chapter_001_plan"][0]
	fake.responses["chapter_001_plan"] = []string{"这不是 JSON"}
	fake.responses["chapter_001_plan_format_repair"] = []string{validPlan}
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
	if fake.calls["chapter_001_plan_format_repair"] != 1 {
		t.Fatal("expected exactly one format repair call")
	}
	assertOneChapterCommitted(t, files, root, 6)
}

func TestMergeReviewOnlyKeepsHardIssueRepairs(t *testing.T) {
	review := story.Review{
		Passed: false,
		Score:  55,
		HardIssues: []story.Issue{{
			Code: "missing_payoff", Suggestion: "补写厂办代表的明确结论",
		}},
		QualityIssues:        []story.Issue{{Code: "weak_transition", Suggestion: "扩写申请过程"}},
		RevisionInstructions: []string{"补写厂办代表的明确结论", "扩写申请过程"},
	}

	merged := mergeDeterministicIssues(review, nil)
	if merged.Passed {
		t.Fatal("存在硬伤时不得通过")
	}
	if len(merged.RevisionInstructions) != 1 || merged.RevisionInstructions[0] != "补写厂办代表的明确结论" {
		t.Fatalf("修订指令应只保留硬伤修复，实际为 %#v", merged.RevisionInstructions)
	}
}

func testProject() story.Project {
	return story.Project{
		Version: story.FormatVersion, Name: "test", Idea: "工程师修复旧机床",
		Provider: "deepseek", Model: "deepseek-v4-flash", TargetChapters: 3,
		ChapterMinChars: 500, ChapterMaxChars: 800, MaxCalls: 10, CreatedAt: time.Unix(0, 0).UTC(),
	}
}

func newHappyFake(project story.Project) *fakeGenerator {
	genesis := testGenesis(project.TargetChapters)
	plan := story.ChapterPlan{
		Number: 1, Title: "旧机床", Purpose: "让主角主动争取第一次机会", ActiveThreads: []string{"comeback"},
		Scenes: []story.ScenePlan{{Order: 1}, {Order: 2}, {Order: 3}},
		Payoff: "修复成功", Hook: "发现异常声音", Forbidden: []string{"提前揭密"}, TargetChars: 600,
	}
	delta := testDelta()
	review := story.Review{Passed: true, Score: 82, HardIssues: []story.Issue{}, QualityIssues: []story.Issue{}, RevisionInstructions: []string{}}
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
	t.Helper()
	state, err := files.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Chapter != 1 || len(state.Facts) != 1 {
		t.Fatalf("unexpected state: %#v", state)
	}
	if _, err := os.Stat(filepath.Join(root, "chapters", "001.md")); err != nil {
		t.Fatal(err)
	}
	usage, err := files.CountUsage()
	if err != nil || usage != expectedUsage {
		t.Fatalf("unexpected usage count: %d, %v", usage, err)
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
