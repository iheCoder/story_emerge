package config

import (
	"fmt"
	"testing"

	"story_emerge/internal/llm"
)

// 验证 YAML 参数沿角色配置传入最终请求，以及省略、显式覆盖和非法值的不同处理。
func TestRoleGenerationConfigReachesRequest(t *testing.T) {
	// 准备全部必需角色，让各场景只改变 Commit 参数，排除无关配置缺失造成的失败。
	base := `providers:
  test: {api_key: secret, endpoint: https://example.test}
roles:
  architect: {provider: test, model: model}
  planner: {provider: test, model: model}
  writer: {provider: test, model: model}
  editor: {provider: test, model: model}
  commit: {provider: test, model: model%s}
`
	for _, tc := range []struct {
		fields string
		tokens int
		effort string
		bad    bool
	}{
		// 省略时 Commit 采用 24000/none；显式配置则覆盖内置默认值。
		{"", 24000, "none", false},
		{", max_output_tokens: 32000, reasoning_effort: high", 32000, "high", false},

		// 显式的 0、负数和未知强度必须在读取配置时被拒绝，不能静默回退默认值。
		{", max_output_tokens: 0", 0, "", true},
		{", max_output_tokens: -1", 0, "", true},
		{", reasoning_effort: unknown", 0, "", true},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			// 从真实 YAML 文件入口加载，覆盖指针字段对“省略”和“填写 0”的区分。
			settings, err := Load(writeConfig(t, fmt.Sprintf(base, tc.fields)))
			if tc.bad {
				if err == nil {
					t.Fatal("无效参数没有被拒绝")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			// 按生产链路合并供应商与角色配置，再解析请求，避免只验证字段能被读取。
			configs, err := settings.RoleConfigs()
			if err != nil {
				t.Fatal(err)
			}
			client, err := llm.NewRoleClient(configs)
			if err != nil {
				t.Fatal(err)
			}
			request, err := client.ResolveRequest(llm.Request{Role: llm.RoleCommit})
			if err != nil || request.MaxOutputTokens != tc.tokens || request.ReasoningEffort != tc.effort {
				t.Fatalf("配置未传入请求: %#v %v", request, err)
			}

			// 格式修复显式要求 none，应优先于角色的 high；未指定的输出上限继续继承角色配置。
			repair, _ := client.ResolveRequest(llm.Request{Role: llm.RoleCommit, ReasoningEffort: "none"})
			if repair.ReasoningEffort != "none" || repair.MaxOutputTokens != tc.tokens {
				t.Fatal("修复参数优先级错误")
			}
		})
	}
}
