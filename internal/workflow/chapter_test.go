package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

type scriptedGenerator struct {
	responses    map[string]string
	inputs       map[string]string
	instructions map[string]string
}

func (fake *scriptedGenerator) Generate(_ context.Context, request llm.Request) (llm.Result, error) {
	fake.inputs[request.Stage] = request.Input
	if fake.instructions != nil {
		fake.instructions[request.Stage] = request.Instructions
	}
	text, ok := fake.responses[request.Stage]
	if !ok {
		return llm.Result{}, fmt.Errorf("unexpected stage %s", request.Stage)
	}
	return llm.Result{Text: text, Model: "fake", ResponseID: request.Stage, Duration: time.Millisecond}, nil
}

func TestChapterFlowUsesEditorAndKeepsReaderIndependent(t *testing.T) {
	// 场景：一个中性旅行故事连续生成两章，Editor 都直接接受。
	// 预期：Writer 与 Editor 能看到上一章读者反馈；下一次 Reader 看不到它，
	// 且每章只应用最终 Editor Story Update 后原子提交。
	fake := newChapterFlowGenerator()
	files := runChapterFlow(t, fake)

	assertAuthorContexts(t, fake)
	assertReaderContext(t, fake)
	assertCommittedChapter(t, files)
}

func TestWriterExperimentGuidanceIsExplicitAndProductionDefaultStaysClean(t *testing.T) {
	// 场景：独立 eval 为 Writer 注入一条研究倾向，而正常生产 Engine 不传实验选项。
	// 预期：实验指导只出现在 Writer/修订上下文；Reader、Editor 权限和持久化结构不变，
	// 空选项也不会向正常 Writer 输入添加实验字段。
	baseline := newChapterFlowGenerator()
	runChapterFlow(t, baseline)
	if strings.Contains(baseline.instructions["chapter_001_write"], "本次独立实验指导") {
		t.Fatal("生产默认 Writer 指令被实验内容污染")
	}

	guidance := "允许安静日常；重要变化优先由人物选择自然产生，不强制制造事件"
	experiment := newChapterFlowGenerator()
	runChapterFlowWithOptions(t, experiment, Options{WriterGuidance: guidance})
	writerInstructions := experiment.instructions["chapter_001_write"]
	if !strings.Contains(writerInstructions, "本次独立实验指导") || !strings.Contains(writerInstructions, guidance) {
		t.Fatalf("实验指导没有显式进入 Writer 系统指令: %s", writerInstructions)
	}
	if strings.Contains(experiment.inputs["chapter_001_reader"], guidance) {
		t.Fatal("实验指导泄露给了独立 Reader")
	}
}

func TestTwoInterventionsEndWithFinalizeAndOnlyFinalState(t *testing.T) {
	// 场景：Editor 对第一稿 revise，对第二稿 replan，正好耗尽两次预算。
	// 预期：第三稿不再进入质量否决，Editor Finalize 只提炼最终稿状态；
	// 两份被否决草稿携带的候选变化不能进入 HEAD。
	genesis := testGenesis()
	replanned := genesis.Outline
	replanned.Version = 1
	replanned.CurrentArc = story.StoryArc{Name: "风雪", Purpose: "让旅程承受第一次真实阻力"}

	revise := story.EditorDecision{
		Action: story.EditorRevise, Reason: "当前行动被说明覆盖", RevisionGuidance: "保留启程选择，让阻力通过行动发生",
		StoryUpdate: story.StoryUpdate{Chapter: 1, DurableStateChanges: []story.DurableStateChange{{Operation: "upsert", ID: "rejected-one", Description: "不应提交"}}, StoryStatus: "ongoing"},
	}
	replan := story.EditorDecision{
		Action: story.EditorReplan, Reason: "当前未来方向已经无法容纳正文变化", ReplanGuidance: "把未来方向调整为人物主动穿越风雪",
		StoryUpdate: story.StoryUpdate{Chapter: 1, DurableStateChanges: []story.DurableStateChange{{Operation: "upsert", ID: "rejected-two", Description: "不应提交"}}, StoryStatus: "ongoing"},
	}
	finalized := story.EditorFinalizeResult{
		StoryUpdate: story.StoryUpdate{Chapter: 1, DurableStateChanges: []story.DurableStateChange{{Operation: "upsert", ID: "final", Description: "阿禾已进入风雪"}}, StoryStatus: "ongoing"},
		Summary:     story.ChapterSummary{Number: 1, Title: "风雪", Summary: "阿禾主动走入风雪"},
	}
	fake := &scriptedGenerator{inputs: map[string]string{}, responses: map[string]string{
		"architect":                          mustJSON(genesis),
		"chapter_001_write":                  "# 第1章 初稿\n\n阿禾出门。",
		"chapter_001_editor_review_1":        mustJSON(revise),
		"chapter_001_revise_1":               "# 第1章 二稿\n\n阿禾推开门，风雪迎面压来。",
		"chapter_001_editor_review_2":        mustJSON(replan),
		"chapter_001_architect_replan":       mustJSON(replanned),
		"chapter_001_rewrite_after_replan_2": "# 第1章 风雪\n\n阿禾没有退回门内，反而把信压紧，走进雪里。",
		"chapter_001_editor_finalize":        mustJSON(finalized),
		"chapter_001_reader":                 mustJSON(story.ReaderObservation{Chapter: 1, TurnPageReason: "想看他怎样穿过风雪"}),
	}}

	files := runOneChapter(t, fake)
	state, err := files.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.DurableStates) != 1 || state.DurableStates[0].ID != "final" {
		t.Fatalf("被否决草稿污染了最终 Story State: %#v", state.DurableStates)
	}
	if state.OutlineVersion != 1 {
		t.Fatalf("replan 未与最终章节同事务提交: %#v", state)
	}
}

func TestFailedFinalizeDoesNotCommitCandidateReplan(t *testing.T) {
	// 场景：两次干预后 Architect 已生成候选 Outline，但 Finalize 返回错误章节号。
	// 预期：整个章节失败，HEAD 和正式 Outline 仍停留在初始化版本。
	genesis := testGenesis()
	replanned := genesis.Outline
	replanned.Version = 1
	revise := story.EditorDecision{Action: story.EditorRevise, Reason: "执行失败", RevisionGuidance: "重写行动"}
	replan := story.EditorDecision{Action: story.EditorReplan, Reason: "方向失效", ReplanGuidance: "调整未来方向"}
	invalidFinalize := story.EditorFinalizeResult{
		StoryUpdate: story.StoryUpdate{Chapter: 99, StoryStatus: "ongoing"},
		Summary:     story.ChapterSummary{Number: 99, Title: "错误", Summary: "错误"},
	}
	fake := &scriptedGenerator{inputs: map[string]string{}, responses: map[string]string{
		"architect":                          mustJSON(genesis),
		"chapter_001_write":                  "# 第1章 初稿\n正文",
		"chapter_001_editor_review_1":        mustJSON(revise),
		"chapter_001_revise_1":               "# 第1章 二稿\n正文",
		"chapter_001_editor_review_2":        mustJSON(replan),
		"chapter_001_architect_replan":       mustJSON(replanned),
		"chapter_001_rewrite_after_replan_2": "# 第1章 三稿\n正文",
		"chapter_001_editor_finalize":        mustJSON(invalidFinalize),
	}}

	root := filepath.Join(t.TempDir(), "novel")
	files := store.New(root)
	project := story.Project{Name: "test", Idea: "送信", LengthProfile: "epic", Provider: "fake", Model: "fake", MaxCalls: 20}
	engine, err := New(fake, files, project.MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), 1); err == nil {
		t.Fatal("无效 Finalize 却允许章节提交")
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 0 || state.OutlineVersion != 0 {
		t.Fatalf("失败章节推进了正式状态: state=%#v err=%v", state, err)
	}
}

func newChapterFlowGenerator() *scriptedGenerator {
	firstObservation := story.ReaderObservation{
		Chapter: 1, UnderstandsGoal: "把信送到山外", TurnPageReason: "想知道山路上会遇见谁",
		CurrentFeeling: "ONLY_AUTHOR_SHOULD_SEE_THIS",
	}
	secondObservation := story.ReaderObservation{Chapter: 2, CurrentFeeling: "紧张", TurnPageReason: "想知道能否走出风雪"}

	return &scriptedGenerator{inputs: map[string]string{}, instructions: map[string]string{}, responses: map[string]string{
		"architect":                   mustJSON(testGenesis()),
		"chapter_001_write":           "# 第1章 启程\n\n阿禾把信压进衣襟，踏上了山路。",
		"chapter_001_editor_review_1": mustJSON(acceptedDecision(1, "启程", "阿禾离开家门")),
		"chapter_001_reader":          mustJSON(firstObservation),
		"chapter_002_write":           "# 第2章 山路\n\n风雪盖住了来路，阿禾只能继续向前。",
		"chapter_002_editor_review_1": mustJSON(acceptedDecision(2, "山路", "阿禾走入风雪")),
		"chapter_002_reader":          mustJSON(secondObservation),
	}}
}

func acceptedDecision(chapter int, title, summary string) story.EditorDecision {
	return story.EditorDecision{
		Action: story.EditorAccept, Reason: "当前章节没有严重到值得干预的问题",
		StoryUpdate: story.StoryUpdate{Chapter: chapter, StoryStatus: "ongoing"},
		Summary:     story.ChapterSummary{Number: chapter, Title: title, Summary: summary},
	}
}

func runChapterFlow(t *testing.T, fake *scriptedGenerator) *store.Store {
	return runChapterFlowWithOptions(t, fake, Options{})
}

func runChapterFlowWithOptions(t *testing.T, fake *scriptedGenerator, options Options) *store.Store {
	t.Helper()

	root := filepath.Join(t.TempDir(), "novel")
	project := story.Project{Name: "test", Idea: "阿禾替朋友送一封信", LengthProfile: "epic", Provider: "fake", Model: "fake", MaxCalls: 20}
	files := store.New(root)
	engine, err := NewWithOptions(fake, files, project.MaxCalls, nil, options)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), 2); err != nil {
		t.Fatal(err)
	}

	return files
}

func runOneChapter(t *testing.T, fake *scriptedGenerator) *store.Store {
	t.Helper()

	root := filepath.Join(t.TempDir(), "novel")
	project := story.Project{Name: "test", Idea: "阿禾替朋友送一封信", LengthProfile: "epic", Provider: "fake", Model: "fake", MaxCalls: 20}
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

	return files
}

func assertAuthorContexts(t *testing.T, fake *scriptedGenerator) {
	t.Helper()

	if strings.Contains(fake.inputs["chapter_001_write"], "chapter_plan") {
		t.Fatal("Writer 仍然收到逐章施工图")
	}
	if !strings.Contains(fake.inputs["chapter_002_write"], "ONLY_AUTHOR_SHOULD_SEE_THIS") {
		t.Fatal("Writer 没有收到上一章 Reader Observation")
	}
	if !strings.Contains(fake.inputs["chapter_002_editor_review_1"], "ONLY_AUTHOR_SHOULD_SEE_THIS") {
		t.Fatal("Editor 没有收到上一章 Reader Observation")
	}
}

func assertReaderContext(t *testing.T, fake *scriptedGenerator) {
	t.Helper()

	input := fake.inputs["chapter_002_reader"]
	for _, forbidden := range []string{
		"story_bible", "story_spine", "narrative_promise", "active_outline",
		"current_story_state", "editor", "ONLY_AUTHOR_SHOULD_SEE_THIS",
	} {
		if strings.Contains(strings.ToLower(input), forbidden) {
			t.Fatalf("Reader 泄露作者侧字段 %q", forbidden)
		}
	}
}

func assertCommittedChapter(t *testing.T, files *store.Store) {
	t.Helper()

	state, err := files.LoadState()
	if err != nil || state.Chapter != 2 {
		t.Fatalf("章节未提交: state=%#v err=%v", state, err)
	}
	summaries, err := files.LoadSummaries()
	if err != nil || len(summaries) != 2 {
		t.Fatalf("独立摘要未按章提交: summaries=%#v err=%v", summaries, err)
	}
}

func testGenesis() story.Genesis {
	character := story.Character{ID: "a_he", Name: "阿禾", Role: "主角", Trait: "谨慎", Desire: "完成托付", Weakness: "从未远行", Secret: "没有读过信"}
	outline := story.StoryOutline{
		Version: 0, CurrentArc: story.StoryArc{Name: "离家", Purpose: "让阿禾独自踏上路途"},
		Tracks: []story.StoryTrack{{ID: "journey", Name: "送信旅程", Role: "推动人物选择", Direction: "从离家走向山外", Status: "ongoing"}},
	}

	return story.Genesis{
		Bible: story.StoryBible{
			Title: "测试书", Premise: "少年替朋友送信", StorySpine: "从依赖家乡到独立选择道路",
			TargetReader:     story.TargetReader{Name: "小岚", ReadingHistory: "长期阅读成长冒险", Craves: []string{"人物选择"}, DropsWhen: []string{"行动没有后果"}},
			NarrativePromise: story.NarrativePromise{PrimaryPleasure: "看普通人独自成长", MustDeliver: []string{"选择产生后果"}, MustNotBecome: []string{"事件互不相干"}},
			Characters:       []story.Character{character}, EndingDirection: "信被送达",
		},
		Outline: outline,
		InitialState: story.InitialState{
			CharacterStates: []story.CharacterState{{CharacterID: "a_he", State: "仍在家门口犹豫"}},
			TrackProgress:   []story.TrackProgress{{TrackID: "journey", Progress: "尚未启程"}},
		},
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
