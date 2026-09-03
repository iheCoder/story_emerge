package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStageFailureContinuesFromCommittedHistory(t *testing.T) {
	// 场景：前两章已经提交，第三章分别在规划、写作、评审、提取或文件提交处中断。
	// 预期：失败不推进 HEAD；新引擎继续时从第三章规划开始，前两章不重写，完结只在成功提交后生效。
	for _, stage := range []string{"plan_1", "write_1", "editor_1_1", "commit", "checkpoint"} {
		t.Run(stage, func(t *testing.T) {
			fake := newFake(3)
			review := accepted()
			review.StoryComplete = true
			fake.responses["chapter_003_editor_1_1"] = mustJSON(review)
			engine, files := initializeTest(t, fake)
			if err := engine.Run(context.Background(), 2); err != nil {
				t.Fatal(err)
			}
			before, err := files.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			bodyOne, _ := files.LoadChapter(1)
			bodyTwo, _ := files.LoadChapter(2)

			// 模型故障直接注入对应角色；文件故障用目录阻止检查点重命名，保留可能已经写出的孤儿产物。
			blocked := filepath.Join(files.Root(), "checkpoints", "003.json")
			if stage == "checkpoint" {
				if err := os.Mkdir(blocked, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				fake.failures["chapter_003_"+stage] = errors.New("模拟阶段中断")
			}
			if err := engine.Run(context.Background(), 1); err == nil {
				t.Fatal("注入的故障没有停止生成")
			}
			stopped, err := files.LoadState()
			if err != nil || !reflect.DeepEqual(stopped, before) {
				t.Fatalf("失败改变了正式状态: %#v %v", stopped, err)
			}
			if _, err := files.LoadChapter(3); err == nil {
				t.Fatal("中断章节被当成正式正文")
			}
			if stage == "checkpoint" {
				if err := os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
			}

			// 新生成器不继承上一次调用的会话。只需旧 HEAD 就能启动，第三章允许形成新的正文版本。
			retry := newFake(3)
			retry.responses["chapter_003_write_1"] = "# 第3章 继续生长\n\n两人决定明天一起去赶集。"
			retry.responses["chapter_003_editor_1_1"] = mustJSON(review)
			resumed, err := New(retry, files, 100, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := resumed.Run(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			want := []string{"chapter_003_plan_1", "chapter_003_write_1", "chapter_003_editor_1_1", "chapter_003_commit"}
			if !reflect.DeepEqual(retry.order, want) {
				t.Fatalf("继续生长调用顺序错误: %v", retry.order)
			}

			// 核对正式历史边界：前两章逐字保留，第三章使用本轮通过的版本，完结标记随提交一起写入。
			after, err := files.LoadState()
			if err != nil || after.Chapter != 3 || !after.Completed {
				t.Fatalf("第三章未正式完结: %#v %v", after, err)
			}
			for n, expected := range map[int]string{1: bodyOne, 2: bodyTwo, 3: retry.responses["chapter_003_write_1"] + "\n"} {
				actual, err := files.LoadChapter(n)
				if err != nil || actual != expected {
					t.Fatalf("第 %d 章正文错误: %q %v", n, actual, err)
				}
			}
			// 空补丁合法，不能因为重新经过提取就误修改已有的故事事实。
			if !reflect.DeepEqual(after.Story, before.Story) {
				t.Fatal("空补丁改变了正式事实")
			}
		})
	}
}
