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
	responses map[string]string
	inputs    map[string]string
}

func (fake *scriptedGenerator) Generate(_ context.Context, request llm.Request) (llm.Result, error) {
	fake.inputs[request.Stage] = request.Input
	text, ok := fake.responses[request.Stage]
	if !ok {
		return llm.Result{}, fmt.Errorf("unexpected stage %s", request.Stage)
	}
	return llm.Result{Text: text, Model: "fake", ResponseID: request.Stage, Duration: time.Millisecond}, nil
}

func TestChapterFlowLetsWriterWriteWithoutPlanAndReaderCannotSeeSecrets(t *testing.T) {
	// 场景：一个普通旅行故事生成首章。测试数据刻意不绑定任何产品题材，
	// 只验证 Writer 自由写作、Reader 上下文隔离和章节原子提交三条规则。
	genesis := testGenesis()
	delta := story.StateDelta{
		Chapter: 1, Summary: story.ChapterSummary{Number: 1, Title: "启程", Summary: "阿禾决定把信送到山外"},
		OutlineProgress: story.OutlineProgress{CurrentMovementID: "depart", Status: "ongoing", Evidence: "已经离家"},
		StoryStatus:     "ongoing",
	}
	readerState := story.ReaderState{
		Chapter: 1, UnderstandsGoal: "把信送到山外", ContinueReason: "想知道山路上会遇见谁",
		LosingPatienceWith: []string{"开头说明稍多"}, SuggestedAction: "continue",
	}
	fake := &scriptedGenerator{inputs: map[string]string{}, responses: map[string]string{
		"architect":          mustJSON(genesis),
		"chapter_001_write":  "# 第1章 启程\n\n阿禾把信压进衣襟，回头看了一眼还亮着灯的屋子。山风正从门缝里钻进来，他知道再等一会儿就走不了了，于是提起鞋边，踏上了那条从未独自走过的路。",
		"chapter_001_record": mustJSON(delta),
		"chapter_001_canon":  mustJSON(story.CanonReview{Passed: true}),
		"chapter_001_reader": mustJSON(readerState),
	}}
	root := filepath.Join(t.TempDir(), "novel")
	project := story.Project{Name: "test", Idea: "阿禾替朋友送一封信", LengthProfile: "epic", Provider: "fake", Model: "fake", MaxCalls: 20}

	// 阶段一：初始化项目，再生成并提交一章。
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

	// 阶段二：Writer 不应收到章节施工图。
	if strings.Contains(fake.inputs["chapter_001_write"], "approved_plan") {
		t.Fatal("writer 仍然收到章节施工图")
	}

	// 阶段三：Reader 能读取目标读者与叙事承诺，但不能读取故事幕后状态。
	readerInput := fake.inputs["chapter_001_reader"]
	for _, forbidden := range []string{"active_outline", "canonical_state", "ending_direction"} {
		if strings.Contains(readerInput, forbidden) {
			t.Fatalf("reader 泄露幕后字段 %q", forbidden)
		}
	}

	// 阶段四：Reader 与正文必须随同一个 HEAD 提交。
	state, err := files.LoadState()
	if err != nil || state.Chapter != 1 {
		t.Fatalf("章节未提交: state=%#v err=%v", state, err)
	}

	// Reader 可以一边对局部内容失去耐心，一边仍愿继续。程序必须保存
	// Reader 的原始判断，不能从一个主观字段推导另一个主观字段。
	savedReader, err := files.LoadReaderState()
	if err != nil || savedReader.SuggestedAction != "continue" {
		t.Fatalf("Reader 主观结论被程序改写: state=%#v err=%v", savedReader, err)
	}
}

func testGenesis() story.Genesis {
	character := story.Character{ID: "a_he", Name: "阿禾", Role: "主角", Trait: "谨慎", Desire: "完成托付", Weakness: "从未远行", Secret: "没有读过信"}
	outline := story.StoryOutline{Version: 0, CoreConflict: "山路即将封闭", ProtagonistDrive: "把信送到山外", CurrentMovementID: "depart",
		Movements: []story.StoryMovement{{ID: "depart", Name: "离家", DramaticPressure: "风雪将至", IntendedChange: "独自启程", ExpectedReward: "迈出第一步", CompletionSignals: []string{"离开村庄"}}}}
	return story.Genesis{
		Bible: story.StoryBible{Title: "测试书", Genre: "旅途故事", Logline: "少年替朋友送信", ProtagonistID: "a_he", Characters: []story.Character{character},
			TargetReader: story.TargetReader{Name: "小岚", ReadingHistory: "长期阅读成长冒险", Craves: []string{"人物选择"}, DropsWhen: []string{"行动没有后果"},
				BingeTriggers: []string{"选择改变关系"}, FitWithStory: "偏爱行动推动人物成长的故事"},
			NarrativePromise: story.NarrativePromise{PrimaryPleasure: "看普通人独自成长", MustDeliver: []string{"选择产生后果"}, MustNotBecome: []string{"事件互不相干"}}, EndingDirection: "信被送达"},
		Outline:            outline,
		InitialState:       story.InitialState{Characters: []story.CharacterState{{CharacterID: "a_he", Goal: "启程", Emotion: "犹豫", Location: "家门口"}}},
		InitialReaderState: story.ReaderState{Chapter: 0, WantsNext: []string{"看阿禾是否真的出发"}, SuggestedAction: "continue"},
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
