package prompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOnlyCurrentRolesAndSchemasAreEmbedded(t *testing.T) {
	// 场景：二进制只携带当前生产角色。
	// 预期：Director 已接入；Planner 与其他旧入口完全消失，避免被后续代码意外调用。
	for _, name := range []string{"architect", "director", "writer", "writer_revision", "editor", "commit"} {
		if _, err := Template(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"planner", "reader", "architect_replan", "editor_finalize", "arc_review", "editor_local"} {
		if _, err := Template(name); err == nil {
			t.Fatalf("旧角色仍嵌入: %s", name)
		}
	}
	for _, name := range []string{"genesis", "director_decision", "editor_decision", "chapter_commit"} {
		data, err := Schema(name)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		assertStrictObjects(t, name, value)
	}
}

func TestDirectorContractHasNoStoryStatusOrChapterPlan(t *testing.T) {
	// 场景：Story Director 是阶段方向维护者，不能取得完结判断或逐章规划职责。
	// 预期：Prompt 与 Schema 都不存在 story_status，且 Prompt 明确禁止生成下一章计划和具体事件。
	template, err := Template("director")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := Schema("director_decision")
	if err != nil {
		t.Fatal(err)
	}

	for source, content := range map[string]string{"Prompt": template, "Schema": string(schema)} {
		if strings.Contains(strings.ToLower(content), "story_status") {
			t.Fatalf("Director %s 仍包含 story_status", source)
		}
	}
	for _, guardrail := range []string{"默认优先 KEEP", "不负责决定下一章具体发生什么", "reader_expectation"} {
		if !strings.Contains(template, guardrail) {
			t.Fatalf("Director Prompt 缺少关键约束: %s", guardrail)
		}
	}
}

func assertStrictObjects(t *testing.T, path string, value any) {
	t.Helper()
	switch node := value.(type) {
	case map[string]any:
		if node["type"] == "object" {
			if node["additionalProperties"] != false {
				t.Fatalf("%s 允许额外字段", path)
			}
			properties := node["properties"].(map[string]any)
			required := node["required"].([]any)
			if len(properties) != len(required) {
				t.Fatalf("%s 不符合严格输出契约", path)
			}
			for _, name := range required {
				if _, ok := properties[name.(string)]; !ok {
					t.Fatalf("未知 required: %s", name)
				}
			}
		}
		for key, child := range node {
			assertStrictObjects(t, path+"."+key, child)
		}
	case []any:
		for _, child := range node {
			assertStrictObjects(t, path, child)
		}
	}
}
