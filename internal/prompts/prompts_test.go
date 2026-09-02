package prompts

import (
	"encoding/json"
	"testing"
)

func TestOnlyCurrentRolesAndSchemasAreEmbedded(t *testing.T) {
	// 场景：新架构发布时只应携带当前可执行的角色与契约。
	// 预期：旧 Reader/Arc/Finalize 入口完全消失，避免被后续代码意外调用。
	for _, name := range []string{"architect", "planner", "writer", "writer_revision", "editor", "commit"} {
		if _, err := Template(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"reader", "architect_replan", "editor_finalize", "arc_review", "editor_local"} {
		if _, err := Template(name); err == nil {
			t.Fatalf("旧角色仍嵌入: %s", name)
		}
	}
	for _, name := range []string{"genesis", "chapter_plan", "editor_decision", "chapter_commit"} {
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
