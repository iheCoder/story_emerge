package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

func TestSpineReachesOnlyDirectorAndSurvivesRestartAndChapterCommit(t *testing.T) {
	// 场景：Architect 建立固定参照，第3章后 Director 调整当前位置；重新启动后继续至第6章。
	// 预期：Spine 随 checkpoint 恢复并穿过普通章节提交，Director 看到最近两章原文；下游只得到当前方向。
	fake := newFake(3)
	for number, body := range []string{"FIRST_OLD_PROSE", "SECOND_PROSE_WITH_HESITATION", "THIRD_PROSE_LIMITED_TRUST"} {
		fake.responses[chapterStage(number+1, "write")] = "# 第" + []string{"1", "2", "3"}[number] + "章 相处\n\n" + body
	}
	updated := testDirection()
	updated.CurrentPosition = "已经愿意倾诉，但重要决定中的信任尚未建立"
	fake.responses["chapter_003_director"] = mustJSON(story.DirectorDecision{
		Action: story.DirectorAdjust, Direction: updated, Reason: "DIRECTOR_AUDIT_ONLY",
	})
	engine, files := initializeTest(t, fake)
	initial, err := files.LoadState()
	if err != nil || !reflect.DeepEqual(initial.StorySpine, testGenesis().StorySpine) {
		t.Fatalf("Architect 参照未进入初态: %#v %v", initial, err)
	}
	if err := engine.Run(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	var input directorInput
	if err := json.Unmarshal([]byte(fake.requests["chapter_003_director"].Input), &input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.StorySpine, initial.StorySpine) || len(input.RecentChapters) != 2 ||
		!strings.Contains(input.RecentChapters[0], "SECOND_PROSE_WITH_HESITATION") || !strings.Contains(input.RecentChapters[1], "THIRD_PROSE_LIMITED_TRUST") {
		t.Fatalf("Director 没有获得正确参照与最近原文: %#v", input)
	}
	if strings.Contains(fake.requests["chapter_003_director"].Input, "FIRST_OLD_PROSE") {
		t.Fatal("Director 最近正文窗口无限扩张")
	}

	// 使用新 Store 和 Engine，排除仅在内存中保存 Spine 的假接线。
	resumedFake := newFake(6)
	resumedFake.responses["chapter_006_director"] = mustJSON(story.DirectorDecision{
		Action: story.DirectorKeep, Direction: updated, Reason: "仍有关系积累价值",
	})
	resumed, err := New(resumedFake, store.New(files.Root()), 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Run(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	final, err := files.LoadState()
	if err != nil || final.Chapter != 6 || final.DirectionVersion != 2 || final.Direction != updated || !reflect.DeepEqual(final.StorySpine, initial.StorySpine) {
		t.Fatalf("续写或 KEEP 丢失了正式规划: %#v %v", final, err)
	}
	if !strings.Contains(resumedFake.requests["chapter_006_director"].Input, "SPINE_FUTURE_SECRET") {
		t.Fatal("后续 Director 没读到 Architect 建立的固定 Spine")
	}
	for stage, request := range resumedFake.requests {
		if request.Role == llm.RoleDirector {
			continue
		}
		for _, forbidden := range []string{"SPINE_FUTURE_SECRET", "DIRECTOR_AUDIT_ONLY", "story_spine", "spine_revision"} {
			if strings.Contains(request.Input, forbidden) {
				t.Fatalf("%s 泄露全书规划或审计: %s", stage, forbidden)
			}
		}
		if request.Role == llm.RoleWriter && !strings.Contains(request.Input, updated.CurrentPosition) {
			t.Fatalf("%s 没有得到当前位置判断", stage)
		}
	}
}

func TestInvalidPlanningOutputCannotChangeFormalState(t *testing.T) {
	// 场景：Architect 未提供完整参照，或 Director 试图越权返回 Spine / 省略当前位置。
	// 预期：严格 Schema/领域校验明确拒绝；初始化失败无 HEAD，Director 失败保留已提交第3章和旧规划。
	for _, scenario := range []string{"missing_initial_spine", "empty_initial_spine", "unauthorized_revision", "unauthorized_spine", "missing_position"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newFake(3)
			stage := "chapter_003_director"
			initialFailure := strings.Contains(scenario, "initial")
			if initialFailure {
				stage = "architect"
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(fake.responses[stage]), &output); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing_initial_spine":
				delete(output, "story_spine")
			case "empty_initial_spine":
				output["story_spine"] = []any{}
			case "unauthorized_revision":
				output["spine_revision"] = testGenesis().StorySpine
			case "unauthorized_spine":
				output["story_spine"] = testGenesis().StorySpine
			case "missing_position":
				delete(output["direction"].(map[string]any), "current_position")
			}
			fake.responses[stage] = mustJSON(output)
			// 越权字段会经过一次格式修复；即使模型坚持返回，也不能进入正式检查点。
			if strings.HasPrefix(scenario, "unauthorized_") {
				fake.responses[stage+"_format_repair"] = mustJSON(output)
			}
			if initialFailure {
				files := store.New(filepath.Join(t.TempDir(), "novel"))
				engine, err := New(fake, files, 100, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := engine.Initialize(context.Background(), testProject()); err == nil {
					t.Fatal("不完整初始化被接受")
				}
				if _, err := os.Stat(filepath.Join(files.Root(), "HEAD")); !os.IsNotExist(err) {
					t.Fatalf("初始化失败仍建立正式 HEAD: %v", err)
				}
				return
			}
			engine, files := initializeTest(t, fake)
			if err := engine.Run(context.Background(), 3); err == nil {
				t.Fatal("不完整规划被接受")
			}
			state, err := files.LoadState()
			if err != nil || state.Chapter != 3 || state.DirectionReviewedAfterChapter != 0 || state.Direction != testDirection() || !reflect.DeepEqual(state.StorySpine, testGenesis().StorySpine) {
				t.Fatalf("失败规划污染正式检查点: %#v %v", state, err)
			}
		})
	}
}

func TestSoftLengthTargetDoesNotForceCompletionOrBlockMoreProse(t *testing.T) {
	// 场景：短篇已经超过3万字，但 Editor 认为需要保留后果展开，尚未确认完结。
	// 预期：Director 得到真实已写字数并可 KEEP，后续章仍能提交；Spine 与篇幅都不能代替 Editor 宣告完成。
	fake := newFake(4)
	fake.responses["chapter_001_write"] = "# 第1章 陪伴\n\n" + strings.Repeat("静", 31000)
	project := testProject()
	project.LengthProfile = "short"
	files := store.New(filepath.Join(t.TempDir(), "novel"))
	engine, err := New(fake, files, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Initialize(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 4 || state.Completed || state.WrittenCharacters <= 30000 {
		t.Fatalf("篇幅软目标变成强制停止/完结: %#v %v", state, err)
	}
	var input directorInput
	if err := json.Unmarshal([]byte(fake.requests["chapter_003_director"].Input), &input); err != nil {
		t.Fatal(err)
	}
	if input.StoryProgress.TargetLength != story.LengthGoal("short") || input.StoryProgress.WrittenCharacters <= 30000 {
		t.Fatalf("Director 没有拿到真实篇幅信号: %#v", input.StoryProgress)
	}
}
