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
