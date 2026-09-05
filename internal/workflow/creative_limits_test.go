package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"story_emerge/internal/story"
)

func TestCreativeGuidanceDoesNotBlockChapterSubmission(t *testing.T) {
	// 场景：Architect 返回少于或多于建议范围的核心承诺，Editor 返回四个修订问题。
	// 使用真实 Schema、领域校验与文件提交流程，确认这些数量不再让生成中断。
	// 空数组也纳入边界：本次删除数量校验，不暗中改成另一种最低条数限制。
	for _, count := range []int{0, 2, 6} {
		t.Run(fmt.Sprintf("promises_%d", count), func(t *testing.T) {
			fake := newFake(1)
			genesis := testGenesis()
			genesis.StoryCore.ReaderPromises = make([]story.ReaderPromise, count)
			for i := range genesis.StoryCore.ReaderPromises {
				genesis.StoryCore.ReaderPromises[i] = story.ReaderPromise{Promise: fmt.Sprintf("承诺%d", i+1), PayoffShape: "通过人物选择及后果兑现"}
			}
			fake.responses["architect"] = mustJSON(genesis)

			// Editor 意见必须完整传给 Writer，不能靠截断数组来满足旧上限。
			issues := []string{"补足选择动机", "交代钥匙来源", "保持人物知情范围", "承接受伤后的行动限制"}
			fake.responses[chapterStage(1, "editor", 1)] = mustJSON(story.EditorDecision{
				ChapterDecision: story.EditorRevise,
				Assessment:      story.EditorAssessment{Contribution: "贡献尚未成立", Sequence: "承接合理", Execution: "正文需要修订"},
				BlockingIssues:  issues, DirectionReview: story.DirectionReviewRequest{},
			})

			// 科幻人物台词和嵌入正文的代码块同时经过初稿、修订与 Editor 路径。
			// 这些字面内容不得在验收前被关键词拦截，也不得在提交时被静默删除。
			chapter := "# 第1章 夜班\n\n机器人说：作为AI，我记得那晚。作为 AI，我也会犯错。\n\n终端亮起：\n```text\n门已关闭\n```\n"
			fake.responses[chapterStage(1, "write")] = chapter
			fake.responses[chapterStage(1, "revise", 1)] = chapter
			fake.responses[chapterStage(1, "editor", 2)] = mustJSON(accepted())
			engine, files := initializeTest(t, fake)
			if err := engine.Run(context.Background(), 1); err != nil {
				t.Fatalf("指导性限制阻断了生成: %v", err)
			}

			// 读取正式历史，验证整章确实提交，而不只是模型响应被解码成功。
			state, err := files.LoadState()
			if err != nil || state.Chapter != 1 {
				t.Fatalf("章节未提交: %#v, %v", state, err)
			}
			saved, err := files.LoadChapter(1)
			if err != nil || strings.TrimSpace(saved) != strings.TrimSpace(chapter) {
				t.Fatalf("正文未完整保留: %q, %v", saved, err)
			}
			var revision struct {
				Context        writerContext `json:"context"`
				BlockingIssues []string      `json:"blocking_issues"`
			}
			if err := json.Unmarshal([]byte(fake.requests[chapterStage(1, "revise", 1)].Input), &revision); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(revision.BlockingIssues, issues) {
				t.Fatalf("修订上下文被截断: %#v", revision)
			}
		})
	}
}

func TestRelaxedCreativeLimitsKeepRequiredContentAndDecisionConsistency(t *testing.T) {
	// 放宽数量不能放宽必要内容和动作语义：逐个构造错误，确认相应边界仍拒绝。
	t.Run("承诺缺少兑现方式", func(t *testing.T) {
		core := testGenesis().StoryCore
		core.ReaderPromises = []story.ReaderPromise{{Promise: "重建信任"}}
		if err := story.ValidateCore(core); err == nil {
			t.Fatal("不完整的承诺被接受")
		}
	})
	t.Run("Direction缺少读者期待", func(t *testing.T) {
		if err := story.ValidateDirection(story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "寻找真相", DesiredShift: "获得证据"}); err == nil {
			t.Fatal("不完整 Direction 被接受")
		}
	})
	t.Run("ACCEPT仍有阻断问题", func(t *testing.T) {
		decision := accepted()
		decision.BlockingIssues = []string{"人物凭空得知真相"}
		if err := story.ValidateEditorDecision(decision); err == nil {
			t.Fatal("存在未解决问题的 ACCEPT 被接受")
		}
	})
	t.Run("退回没有问题说明", func(t *testing.T) {
		decision := accepted()
		decision.ChapterDecision = story.EditorRevise
		if err := story.ValidateEditorDecision(decision); err == nil {
			t.Fatal("没有修订问题的退回被接受")
		}
	})
	t.Run("Direction复查请求缺少原因", func(t *testing.T) {
		decision := accepted()
		decision.DirectionReview.Requested = true
		if err := story.ValidateEditorDecision(decision); err == nil {
			t.Fatal("没有原因的 Direction Review 请求被接受")
		}
	})
	t.Run("正文没有章节标题", func(t *testing.T) {
		if err := validateDraft("机器人说：作为 AI，我记得那晚。"); err == nil {
			t.Fatal("缺少必要标题的正文被接受")
		}
	})
}
