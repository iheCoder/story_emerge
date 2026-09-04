package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"story_emerge/internal/llm"
)

func TestLoadResolvesIndependentRoleModels(t *testing.T) {
	// 场景：五个模型职责共享两个供应商，但每个角色有自己的模型绑定。
	// 预期：YAML 能被严格读取，角色配置会继承正确的 API Key/端点，且不会把角色模型串用。
	path := writeConfig(t, `
providers:
  deepseek:
    api_key: deepseek-secret
    endpoint: https://deepseek.test/responses
  openai:
    api_key: openai-secret
    endpoint: https://openai.test/v1/responses
roles:
  architect:
    provider: deepseek
    model: architect-model
  planner:
    provider: openai
    model: planner-model
  writer:
    provider: deepseek
    model: writer-model
  editor:
    provider: openai
    model: editor-model
  commit:
    provider: openai
    model: commit-model
`)

	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := settings.RoleConfigs()
	if err != nil {
		t.Fatal(err)
	}

	if got := configs[llm.RolePlanner]; got.Model != "planner-model" || got.Provider != "openai" {
		t.Fatal("Planner 没有独立模型绑定")
	}
	if got := configs[llm.RoleWriter]; got.Model != "writer-model" || got.Provider != "deepseek" || got.APIKey != "deepseek-secret" {
		t.Fatalf("Writer 配置解析错误: %#v", got)
	}
	if got := configs[llm.RoleCommit]; got.Model != "commit-model" || got.Provider != "openai" || got.Endpoint != "https://openai.test/v1/responses" {
		t.Fatalf("Commit 配置解析错误: %#v", got)
	}
}

func TestLoadAllowsOptionalUnwiredDirectorRole(t *testing.T) {
	// 场景：用户希望单独试验已经实现的 Story Director，但生产章节循环仍只要求原有五个角色。
	// 预期：配置层识别 director 并建立独立模型绑定；没有 director 的旧配置仍由上一个测试证明可正常加载。
	path := writeConfig(t, `
providers:
  test:
    api_key: secret
    endpoint: https://example.test/responses
roles:
  architect: {provider: test, model: architect-model}
  planner: {provider: test, model: planner-model}
  director: {provider: test, model: director-model}
  writer: {provider: test, model: writer-model}
  editor: {provider: test, model: editor-model}
  commit: {provider: test, model: commit-model}
`)

	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := settings.RoleConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if got := configs[llm.RoleDirector]; got.Model != "director-model" || got.Provider != "test" {
		t.Fatalf("Director 没有获得独立模型绑定: %#v", got)
	}
}

func TestLoadRejectsUnknownFieldsAndMissingRequiredRoles(t *testing.T) {
	// 场景：配置文件出现拼写错误，或漏掉工作流必须使用的 Commit 角色。
	// 预期：启动前失败，避免程序静默使用零值或把错误配置带到模型调用阶段。
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unknown field",
			content: `
providers:
  deepseek:
    api_key: secret
    endpoint: https://deepseek.test/responses
roles:
  architect: {provider: deepseek, model: model}
  planner: {provider: deepseek, model: planner-model}
  writer: {provider: deepseek, model: model}
  editor: {provider: deepseek, model: model}
  commit: {provider: deepseek, model: model}
extra: true
`,
			want: "field extra not found",
		},
		{
			name: "missing commit",
			content: `
providers:
  deepseek:
    api_key: secret
    endpoint: https://deepseek.test/responses
roles:
  architect: {provider: deepseek, model: model}
  planner: {provider: deepseek, model: planner-model}
  writer: {provider: deepseek, model: model}
  editor: {provider: deepseek, model: model}
`,
			want: "缺少必需角色 commit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误不符合预期: err=%v want=%q", err, test.want)
			}
		})
	}
}

func TestRoleConfigsRejectUnknownProvider(t *testing.T) {
	// 场景：角色拼写了不存在的供应商名称。
	// 预期：配置解析阶段明确指出绑定错误，不生成缺少 API Key 的空客户端配置。
	path := writeConfig(t, `
providers:
  deepseek:
    api_key: secret
    endpoint: https://deepseek.test/responses
roles:
  architect: {provider: missing, model: model}
  planner: {provider: deepseek, model: planner-model}
  writer: {provider: deepseek, model: model}
  editor: {provider: deepseek, model: model}
  commit: {provider: deepseek, model: model}
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "角色 architect 引用了未配置的供应商 missing") {
		t.Fatalf("未拒绝未知供应商: %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemovedReaderRoleIsRejected(t *testing.T) {
	// 场景：配置仍保留已删除的 Reader 角色，即使新的五个职责都已配置。
	// 预期：明确拒绝旧绑定，不留下一个看似生效却没有消费者的配置入口。
	roles := map[string]RoleSettings{}
	for _, role := range requiredRoles {
		roles[role] = RoleSettings{Provider: "test", Model: "model"}
	}
	roles["reader"] = RoleSettings{Provider: "test", Model: "model"}
	settings := Settings{Providers: map[string]ProviderSettings{"test": {APIKey: "test", Endpoint: "https://example.test"}}, Roles: roles}
	if _, err := settings.RoleConfigs(); err == nil || !strings.Contains(err.Error(), "未知工作流角色 reader") {
		t.Fatalf("旧角色没有拒绝: %v", err)
	}
}
