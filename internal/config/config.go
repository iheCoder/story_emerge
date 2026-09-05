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
// Providers 负责连接和鉴权，Roles 负责模型绑定以及各角色的生成参数默认值。
type Settings struct {
	Providers map[string]ProviderSettings `yaml:"providers"`
	Roles     map[string]RoleSettings     `yaml:"roles"`
}

// ProviderSettings 描述一个 Responses API 兼容供应商。
type ProviderSettings struct {
	APIKey   string `yaml:"api_key"`
	Endpoint string `yaml:"endpoint"`
}

// RoleSettings 描述角色的模型绑定和生成参数；省略的生成参数由 LLM 层按角色补默认值。
type RoleSettings struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`

	// 空值表示采用角色默认强度；none 表示明确关闭思考，两者含义不同。
	ReasoningEffort string `yaml:"reasoning_effort"`

	// 指针区分“没配置”和“明确填了 0”：前者采用默认值，后者是配置错误。
	MaxOutputTokens *int `yaml:"max_output_tokens"`
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
	// Settings 也可能由代码构造，不能假定调用方一定经过 Load 的校验。
	if err := settings.validate(); err != nil {
		return nil, err
	}

	// 用相同的名称规范化规则处理供应商定义与角色引用，保证合并时能准确匹配。
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
		// 连接凭据来自供应商，模型与生成参数来自角色；同一供应商的不同角色互不覆盖。
		provider := providers[binding.Provider]
		configs[role] = llm.Config{
			Provider:        providerName(binding.Provider),
			Model:           strings.TrimSpace(binding.Model),
			Endpoint:        strings.TrimSpace(provider.Endpoint),
			APIKey:          strings.TrimSpace(provider.APIKey),
			ReasoningEffort: binding.ReasoningEffort,
		}

		// 只复制显式配置的上限；省略时保留内部的 0，交给请求解析统一补角色默认值。
		// map 中的结构体是值，修改局部副本后需要写回。
		if binding.MaxOutputTokens != nil {
			config := configs[role]
			config.MaxOutputTokens = *binding.MaxOutputTokens
			configs[role] = config
		}
	}
	return configs, nil
}

// 一次校验齐全，避免前面的角色已经花费生成成本，后面的必需角色却没有配置。
var requiredRoles = []string{
	llm.RoleArchitect,
	llm.RoleDirector,
	llm.RoleWriter,
	llm.RoleEditor,
	llm.RoleCommit,
}

// validate 校验配置结构与引用关系，不连接供应商，也不验证远端模型是否可用。
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

	// 既拒绝拼错的角色名，也拒绝悬空的供应商引用。
	for role, binding := range roles {
		if role != llm.RoleArchitect && role != llm.RoleDirector && role != llm.RoleWriter && role != llm.RoleEditor && role != llm.RoleCommit {
			return fmt.Errorf("未知工作流角色 %s", role)
		}
		if _, ok := providers[binding.Provider]; !ok {
			return fmt.Errorf("角色 %s 引用了未配置的供应商 %s", role, binding.Provider)
		}
	}

	// 未知角色和缺失角色分别检查，错误信息指向需要修正的配置项。
	for _, role := range requiredRoles {
		if _, ok := roles[role]; !ok {
			return fmt.Errorf("缺少必需角色 %s", role)
		}
	}
	return nil
}

// normalizedProviders 建立统一的供应商索引，同时检查最基本的连接凭据是否齐备。
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

// normalizedRoles 统一角色名称和参数写法，但不在配置读取阶段填充角色默认值。
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

		// YAML 中只要填写了上限，就必须是正数；不能用 0 表示无限输出或关闭输出。
		if role.MaxOutputTokens != nil && *role.MaxOutputTokens <= 0 {
			return nil, fmt.Errorf("角色 %s 的 max_output_tokens 必须大于 0", normalized)
		}

		// 接受大小写与首尾空格差异，但拒绝无法识别的强度，避免配置看似生效。
		effort := strings.ToLower(strings.TrimSpace(role.ReasoningEffort))
		if err := llm.ValidateGenerationSettings(llm.Config{ReasoningEffort: effort}); err != nil {
			return nil, fmt.Errorf("角色 %s: %w", normalized, err)
		}

		// 保留“省略参数”的信息，后续仍能区分角色配置与内置默认值。
		roles[normalized] = RoleSettings{
			Provider:        strings.ToLower(strings.TrimSpace(role.Provider)),
			Model:           strings.TrimSpace(role.Model),
			ReasoningEffort: effort, MaxOutputTokens: role.MaxOutputTokens,
		}
	}

	return roles, nil
}

func providerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
