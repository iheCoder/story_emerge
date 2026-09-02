// Package config 负责读取应用级 YAML 配置，并把供应商配置解析为按角色使用的 LLM 配置。
//
// API Key 只在读取配置后进入内存；项目目录只保存故事本身，不保存连接凭据或模型选择。
package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"story_emerge/internal/llm"
)

// Settings 是 config.yaml 的完整结构。
// Providers 负责连接和鉴权，Roles 只负责把工作流角色绑定到某个供应商模型。
type Settings struct {
	Providers map[string]ProviderSettings `yaml:"providers"`
	Roles     map[string]RoleSettings     `yaml:"roles"`
}

// ProviderSettings 描述一个 Responses API 兼容供应商。
type ProviderSettings struct {
	APIKey   string `yaml:"api_key"`
	Endpoint string `yaml:"endpoint"`
}

// RoleSettings 描述一个长期工作流角色使用的供应商和模型。
type RoleSettings struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// Load 从指定路径读取并严格校验 YAML 配置。
// 未知字段直接报错，避免用户把拼写错误当成已经生效的配置。
func Load(path string) (Settings, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Settings{}, fmt.Errorf("模型配置文件路径不能为空")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("读取模型配置文件 %s 失败: %w", path, err)
	}

	var settings Settings
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("解析模型配置文件 %s 失败: %w", path, err)
	}

	if err := settings.validate(); err != nil {
		return Settings{}, fmt.Errorf("模型配置文件 %s 无效: %w", path, err)
	}
	return settings, nil
}

// RoleConfigs 把 YAML 中分离的供应商和角色绑定合并为可直接创建客户端的配置。
// 只返回角色配置，调用方无需再次处理 API Key 与端点的匹配关系。
func (settings Settings) RoleConfigs() (map[string]llm.Config, error) {
	if err := settings.validate(); err != nil {
		return nil, err
	}

	providers, err := normalizedProviders(settings.Providers)
	if err != nil {
		return nil, err
	}
	roles, err := normalizedRoles(settings.Roles)
	if err != nil {
		return nil, err
	}

	configs := make(map[string]llm.Config, len(roles))
	for role, binding := range roles {
		provider := providers[binding.Provider]
		configs[role] = llm.Config{
			Provider: providerName(binding.Provider),
			Model:    strings.TrimSpace(binding.Model),
			Endpoint: strings.TrimSpace(provider.Endpoint),
			APIKey:   strings.TrimSpace(provider.APIKey),
		}
	}
	return configs, nil
}

var requiredRoles = []string{
	llm.RoleArchitect,
	llm.RoleWriter,
	llm.RoleEditor,
	llm.RolePlanner,
	llm.RoleCommit,
}

func (settings Settings) validate() error {
	if len(settings.Providers) == 0 {
		return fmt.Errorf("至少配置一个 providers")
	}
	if len(settings.Roles) == 0 {
		return fmt.Errorf("至少配置一个 roles")
	}
	providers, err := normalizedProviders(settings.Providers)
	if err != nil {
		return err
	}
	roles, err := normalizedRoles(settings.Roles)
	if err != nil {
		return err
	}
	for role, binding := range roles {
		if role != llm.RoleArchitect && role != llm.RolePlanner && role != llm.RoleWriter && role != llm.RoleEditor && role != llm.RoleCommit {
			return fmt.Errorf("未知工作流角色 %s", role)
		}
		if _, ok := providers[binding.Provider]; !ok {
			return fmt.Errorf("角色 %s 引用了未配置的供应商 %s", role, binding.Provider)
		}
	}
	for _, role := range requiredRoles {
		if _, ok := roles[role]; !ok {
			return fmt.Errorf("缺少必需角色 %s", role)
		}
	}
	return nil
}

func normalizedProviders(input map[string]ProviderSettings) (map[string]ProviderSettings, error) {
	providers := make(map[string]ProviderSettings, len(input))
	for name, provider := range input {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, fmt.Errorf("providers 中存在空名称")
		}
		if _, exists := providers[normalized]; exists {
			return nil, fmt.Errorf("providers 中存在重复名称 %q", normalized)
		}
		if strings.TrimSpace(provider.APIKey) == "" {
			return nil, fmt.Errorf("供应商 %s 缺少 api_key", normalized)
		}
		if strings.TrimSpace(provider.Endpoint) == "" {
			return nil, fmt.Errorf("供应商 %s 缺少 endpoint", normalized)
		}
		providers[normalized] = provider
	}
	return providers, nil
}

func normalizedRoles(input map[string]RoleSettings) (map[string]RoleSettings, error) {
	roles := make(map[string]RoleSettings, len(input))
	for name, role := range input {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, fmt.Errorf("roles 中存在空名称")
		}
		if _, exists := roles[normalized]; exists {
			return nil, fmt.Errorf("roles 中存在重复名称 %q", normalized)
		}
		if strings.TrimSpace(role.Provider) == "" {
			return nil, fmt.Errorf("角色 %s 缺少 provider", normalized)
		}
		if strings.TrimSpace(role.Model) == "" {
			return nil, fmt.Errorf("角色 %s 缺少 model", normalized)
		}
		roles[normalized] = RoleSettings{
			Provider: strings.ToLower(strings.TrimSpace(role.Provider)),
			Model:    strings.TrimSpace(role.Model),
		}
	}

	return roles, nil
}

func providerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
