package prompts

import (
	"strings"
	"testing"
)

func TestProductionRolesContainOnlyConvergedArchitecture(t *testing.T) {
	// 场景：架构已经收敛为 Architect、Writer、Editor、Reader 四类长期职责。
	// 预期：新模板可加载；旧的检查、记账或独立重规划角色不再存在。
	for _, name := range []string{"architect", "architect_replan", "writer", "writer_revision", "editor", "editor_finalize", "reader"} {
		if _, err := Template(name); err != nil {
			t.Fatalf("缺少生产 Prompt %s: %v", name, err)
		}
	}
	for _, removed := range []string{"canon_checker", "recorder", "replanner", "revision"} {
		if _, err := Template(removed); err == nil {
			t.Fatalf("旧角色 Prompt 仍存在: %s", removed)
		}
	}
}

func TestProductionPromptsDoNotContainExperimentSpecificRules(t *testing.T) {
	// 场景：同一 Agent 需要服务彼此完全不同的小说。
	// 预期：生产 Prompt 只表达通用权限边界，不包含任何实验样本题材或人物特判。
	for _, name := range []string{"architect", "architect_replan", "writer", "writer_revision", "editor", "editor_finalize", "reader"} {
		content, err := Template(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"刑侦", "DNA", "病房", "旧案", "攻受", "恋爱必须", "爽文必须"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("Prompt %s 包含实验样本特判 %q", name, forbidden)
			}
		}
	}
}

func TestStructuredSchemasMatchNewPersistentArtifacts(t *testing.T) {
	// 场景：模型阶段启动前检查所有结构化输出契约。
	// 预期：初始化、Editor、Reader 和按需重规划 Schema 都能从嵌入资源读取。
	for _, name := range []string{"genesis", "story_outline", "editor_decision", "editor_finalize", "reader_observation"} {
		if _, err := Schema(name); err != nil {
			t.Fatalf("缺少 Schema %s: %v", name, err)
		}
	}
}

func TestSchemasDoNotReintroduceRemovedDuplicateState(t *testing.T) {
	// 场景：结构减法已经把 Track Progress、重复 Editor 意见和九字段 Reader 收缩。
	// 预期：结构化契约不再要求旧字段，避免模型继续生成已经没有消费者的数据。
	for _, name := range []string{"genesis", "story_outline", "editor_decision", "editor_finalize", "reader_observation"} {
		content, err := Schema(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, removed := range []string{
			"track_progress", "future_directions", "completion_signals", "revision_guidance",
			"replan_guidance", "observations", "turn_page_reason", "current_feeling",
		} {
			if strings.Contains(string(content), removed) {
				t.Fatalf("Schema %s 仍包含已删除字段 %q", name, removed)
			}
		}
	}
}

func TestLiveTensionPromptsPreserveCreativeFreedom(t *testing.T) {
	// 场景：Live Tension 已进入 Writer 与 Editor，但不能退化成章节任务或封闭故事名单。
	// 预期：Writer 可以完全不触碰张力并继续引入新内容；Editor 明确把容量当上限，
	// Reader Prompt 则完全不知道这份作者侧状态。
	writer, err := Template("writer")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"本章一项都不碰", "不能成为封闭名单", "不是事实、任务、大纲"} {
		if !strings.Contains(writer, required) {
			t.Fatalf("Writer Prompt 缺少 Live Tension 自由边界 %q", required)
		}
	}

	editor, err := Template("editor")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"空列表合法", "容量只是安全上限而不是填写目标", "不得因为本章没有提及就自动删除"} {
		if !strings.Contains(editor, required) {
			t.Fatalf("Editor Prompt 缺少 Live Tension 维护边界 %q", required)
		}
	}

	reader, err := Template("reader")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(reader), "live tension") {
		t.Fatal("Reader Prompt 泄露了作者侧 Live Tension")
	}

	// 历史实验里 Flash 会把 Schema 的 maxItems 当成填写目标。容量继续由 Go 守住，
	// 但不把具体数字暴露为模型的隐形配额。
	for _, name := range []string{"editor_decision", "editor_finalize"} {
		schema, err := Schema(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(schema), "maxItems") {
			t.Fatalf("Schema %s 重新暴露了 Live Tension 填写配额", name)
		}
	}
}
