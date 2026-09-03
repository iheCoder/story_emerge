package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"story_emerge/internal/llm"
)

// 用脚本化生成器重现本次现场：正文通过审核，Commit 返回携带半截 JSON 的失败响应。
type incompleteCommitGenerator struct{ *scriptedGenerator }

func (fake incompleteCommitGenerator) Generate(ctx context.Context, request llm.Request) (llm.Result, error) {
	if strings.HasSuffix(request.Stage, "_commit") {
		return llm.Result{
			Text: `{"state_patch":`, Model: "test-model", ResponseID: "incomplete-response",
			Usage: llm.Usage{InputTokens: 12000, OutputTokens: 10000},
		}, errors.New("输出不完整: max_output_tokens")
	}
	return fake.scriptedGenerator.Generate(ctx, request)
}

func TestRuntimeLogPreservesFailureAndAppendsAfterRestart(t *testing.T) {
	// 初始化之后令 Commit 截断，检查磁盘日志能单独解释失败，且 HEAD 不变。
	fake := newFake(1)
	engine, files := initializeTest(t, fake)
	engine.generator = incompleteCommitGenerator{fake}
	if err := engine.Run(context.Background(), 1); err == nil {
		t.Fatal("不完整 Commit 被当成成功")
	}
	path := filepath.Join(files.Root(), "runtime.log")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var foundFailure, foundOutcome bool
	for _, line := range strings.Split(strings.TrimSpace(string(before)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["msg"] == "模型请求失败" {
			foundFailure = entry["stage"] == "chapter_001_commit" && entry["role"] == "commit" &&
				entry["response_id"] == "incomplete-response" && entry["output_tokens"] == float64(10000) &&
				entry["error"] == "输出不完整: max_output_tokens" && entry["duration_ms"] != nil
		}
		if entry["stage"] == "run" && entry["level"] == "ERROR" {
			foundOutcome = strings.Contains(entry["error"].(string), "chapter_001_commit")
		}
	}
	if !foundFailure || !foundOutcome {
		t.Fatalf("日志缺少实际失败及本轮终止原因: %s", before)
	}
	if strings.Contains(string(before), "RAW_USER_IDEA_ONLY") || strings.Contains(string(before), `{"state_patch":`) {
		t.Fatal("运行日志不应包含原始输入或模型正文")
	}
	partial, err := os.ReadFile(filepath.Join(files.Root(), ".work", "001-chapter_001_commit-incomplete.txt"))
	if err != nil || string(partial) != `{"state_patch":` {
		t.Fatalf("未保留不完整响应: %q %v", partial, err)
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 0 {
		t.Fatalf("失败响应推进了 HEAD: %#v %v", state, err)
	}

	// 构造全新引擎模拟进程重启。成功续写后，原有失败日志必须逐字保留。
	restarted, err := New(fake, files, testProject().MaxCalls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(after), string(before)) || len(after) <= len(before) {
		t.Fatalf("重启后日志未追加: %v", err)
	}
	if !strings.Contains(string(after[len(before):]), "本轮运行结束") {
		t.Fatal("成功运行缺少结束记录")
	}
}

func TestRuntimeLogRecordsValidationFailureAfterSuccessfulModelCall(t *testing.T) {
	// 模型请求成功不等于工作流成功：合法 JSON 缺少必要字段时仍应持久化最终错误。
	fake := newFake(1)
	fake.responses["chapter_001_commit"] = `{}`
	engine, files := initializeTest(t, fake)
	err := engine.Run(context.Background(), 1)
	if err == nil {
		t.Fatal("缺少字段的结果未失败")
	}
	data, readErr := os.ReadFile(filepath.Join(files.Root(), "runtime.log"))
	if readErr != nil || !strings.Contains(string(data), "运行失败") || !strings.Contains(string(data), "state_patch") {
		t.Fatalf("字段错误没有写入日志: %s %v", data, readErr)
	}
}
