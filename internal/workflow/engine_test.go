package workflow

import (
	"errors"
	"strings"
	"testing"

	"story_emerge/internal/llm"
)

func TestJSONRepairRequestIncludesSchemaTypeFailure(t *testing.T) {
	// 场景：模型返回合法 JSON，但把 Schema 要求的字符串数组写成字符串。
	// 预期：修复请求包含精确类型错误，不能只要求修复括号和逗号。
	request := llm.Request{Stage: "chapter_004_commit", Role: llm.RoleCommit, ReasoningEffort: "low"}
	decodeErr := errors.New("cannot unmarshal string into facts of type []string")

	repaired := jsonRepairRequest(request, `{"facts":"回报"}`, decodeErr)

	if repaired.Stage != "chapter_004_commit_format_repair" || repaired.ReasoningEffort != "none" {
		t.Fatalf("修复阶段配置错误: %#v", repaired)
	}
	if repaired.Role != llm.RoleCommit {
		t.Fatalf("格式修复丢失原角色: %#v", repaired)
	}
	if !strings.Contains(repaired.Input, decodeErr.Error()) || !strings.Contains(repaired.Input, `"facts":"回报"`) {
		t.Fatalf("修复请求缺少解析错误或原答案: %s", repaired.Input)
	}
}

func TestMalformedOutputUsesChapterDiagnosticPrefix(t *testing.T) {
	// 场景：第二章 Commit 返回了错误字段类型。
	// 预期：诊断文件归入第 2 章前缀，而不是混进初始化阶段的 000 现场。
	if got := chapterNumberFromStage("chapter_002_commit_format_repair"); got != 2 {
		t.Fatalf("故障章节解析错误: %d", got)
	}
	if got := chapterNumberFromStage("architect"); got != 0 {
		t.Fatalf("初始化阶段被误判成章节: %d", got)
	}
}

func TestNormalizeChapterHeadingOnlyFixesExistingChapterTitle(t *testing.T) {
	// 场景：Writer 已给出明确中文章节名，但误用二级 Markdown 标题。
	// 预期：程序只把标题层级归一化为一级；正文完全保留。真正没有标题的正文不被猜测修补。
	got := normalizeChapterHeading("## 第二章 声音\n\n正文。\n")
	if got != "# 第二章 声音\n\n正文。\n" {
		t.Fatalf("章节标题归一化错误: %q", got)
	}
	plain := "严娜推开门。\n"
	if normalizeChapterHeading(plain) != plain {
		t.Fatal("程序为无标题正文猜测了章节名")
	}
}
