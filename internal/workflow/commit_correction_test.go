package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"story_emerge/internal/story"
)

func TestCommitCorrectionUsesEvidenceAndStopsAfterOneAttempt(t *testing.T) {
	// 场景：Commit 伪造新增条目 ID 或同时更新删除；纠正可成功、仍失败或被预算阻止。
	// 预期：只让 Commit 纠正一次，保留两次候选与正式事实，失败不能推进 HEAD。
	for _, scenario := range []string{"unknown_id", "update_remove", "still_invalid", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newFake(2)
			engine, files := initializeTest(t, fake)
			// 先提交第一章，让第二章正常提取和纠正都读取真实保存的近期轨迹。
			if err := engine.Run(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			before, err := files.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			person := before.Story.Characters[0]
			bad := extracted()
			change := story.CharacterPatch{ID: person.ID, Name: person.Name,
				Facts:                    story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{{ID: "invented-hash", Value: "已收到信件"}}},
				KnowledgeAndBeliefs:      story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{}, Remove: []string{}},
				CommitmentsAndIntentions: story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{}, Remove: []string{}},
			}
			change.Facts.Remove = []string{}
			if scenario == "update_remove" {
				change.Facts.Upsert[0].ID = person.Facts[0].ID
				change.Facts.Remove = []string{person.Facts[0].ID}
			}
			bad.StatePatch.Characters.Upsert = []story.CharacterPatch{change}
			good := extracted()
			corrected := change
			corrected.Facts = story.CollectionPatch[story.StateItem]{Upsert: []story.StateItem{{Value: "已收到信件"}}, Remove: []string{}}
			if scenario == "update_remove" {
				corrected.Facts.Upsert[0].ID = person.Facts[0].ID
			}
			good.StatePatch.Characters.Upsert = []story.CharacterPatch{corrected}
			fake.responses["chapter_002_commit"] = mustJSON(bad)
			fake.responses["chapter_002_commit_correction"] = mustJSON(good)
			if scenario == "still_invalid" {
				fake.responses["chapter_002_commit_correction"] = mustJSON(bad)
			}
			if scenario == "budget" {
				engine.maxCalls = engine.usedCalls + 1
			}
			got, err := engine.extractAccepted(context.Background(), 2, before, "# 第2章 信件\n\n他收到了信件。")
			if scenario == "still_invalid" || scenario == "budget" {
				if err == nil {
					t.Fatal("失败或预算耗尽仍然返回可提交结果")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				next, err := story.ApplyPatch(before.Story, got.StatePatch)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, fact := range next.Characters[0].Facts {
					if fact.Value == "已收到信件" && fact.ID != "" {
						found = true
					}
				}
				if !found {
					t.Fatal("纠正后丢失必要变化")
				}
			}
			// 提取组件本身不能落正式状态，两轮都不允许触碰小说 HEAD。
			after, err := files.LoadState()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("提取提前修改正式状态")
			}
			raw, err := os.ReadFile(filepath.Join(files.Root(), ".work", "002-chapter_002_commit.json"))
			if err != nil {
				t.Fatal(err)
			}
			var saved story.CommitResult
			if err := json.Unmarshal(raw, &saved); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved, bad) {
				t.Fatal("原始失败候选被修改")
			}
			request, called := fake.requests["chapter_002_commit_correction"]
			if scenario == "budget" {
				if called {
					t.Fatal("纠正绕过调用预算")
				}
				return
			}
			if !called {
				t.Fatal("状态操作错误没有进入纠正")
			}
			for _, key := range []string{"previous_current_story_state", "accepted_chapter", "story_spine", "current_direction", "recent_trajectory", "rejected_extraction", "validation_error"} {
				if !strings.Contains(request.Input, key) {
					t.Fatalf("纠正缺少 %s", key)
				}
			}
			// 两轮的五项业务输入必须完全一致，纠正只额外获得失败输出和校验错误。
			var initialInput, correctionInput map[string]json.RawMessage
			if err := json.Unmarshal([]byte(fake.requests["chapter_002_commit"].Input), &initialInput); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(request.Input), &correctionInput); err != nil {
				t.Fatal(err)
			}
			if len(initialInput) != 5 || len(correctionInput) != 7 {
				t.Fatal("Commit 或纠正输入字段超出约定")
			}
			for key, value := range initialInput {
				if !reflect.DeepEqual(value, correctionInput[key]) {
					t.Fatalf("纠正改变了原始输入 %s", key)
				}
			}
			var received commitInput
			if err := json.Unmarshal([]byte(request.Input), &received); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(received.StorySpine, before.StorySpine) || !reflect.DeepEqual(received.CurrentDirection, before.Direction) || !reflect.DeepEqual(received.RecentTrajectory, before.RecentTrajectory) {
				t.Fatal("纠正缺少实际规划与轨迹内容")
			}
			for _, key := range []string{"user_idea", "story_core", "chapter_ledger"} {
				if strings.Contains(request.Input, key) {
					t.Fatalf("纠正越权读取 %s", key)
				}
			}
			count := 0
			for _, stage := range fake.order {
				if strings.Contains(stage, "correction") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("纠正调用失去边界: %d", count)
			}
		})
	}
}
