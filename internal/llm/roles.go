package llm

import (
	"context"
	"fmt"
	"strings"
)

// 工作流长期角色名称由配置和请求共同使用，避免角色路由依赖阶段字符串猜测。
const (
	RoleArchitect = "architect"
	RoleWriter    = "writer"
	RoleEditor    = "editor"
	RolePlanner   = "planner"
	RoleDirector  = "director"
	RoleCommit    = "commit"
)

// RoleClient 按工作流角色持有独立的模型客户端。
// 同一角色的重规划、修订和格式修复请求都会复用该角色的客户端配置。
type RoleClient struct {
	clients map[string]*Client
}

// NewRoleClient 为每个配置角色创建客户端，并在启动阶段一次性校验连接信息。
func NewRoleClient(configs map[string]Config) (*RoleClient, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("没有配置任何角色模型")
	}

	clients := make(map[string]*Client, len(configs))
	for role, config := range configs {
		// 先统一角色名称，避免大小写或空格导致配置重复、请求却路由到不同客户端。
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			return nil, fmt.Errorf("角色名称不能为空")
		}
		if _, exists := clients[role]; exists {
			return nil, fmt.Errorf("角色模型配置重复: %s", role)
		}

		// 启动时检查参数形状与连接配置，避免小说写到该角色时才发现配置错误。
		// 此处不发网络请求，不能验证服务端是否支持某个模型或思考强度。
		if err := ValidateGenerationSettings(config); err != nil {
			return nil, fmt.Errorf("角色 %s: %w", role, err)
		}
		client, err := NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("角色 %s 的模型配置无效: %w", role, err)
		}

		// 即使多个角色共用同一供应商，也各自保留模型和生成参数。
		clients[role] = client
	}

	return &RoleClient{clients: clients}, nil
}

// Generate 根据请求中的角色选择客户端；空角色或未配置角色都直接失败。
func (client *RoleClient) Generate(ctx context.Context, request Request) (Result, error) {
	// 直接调用 RoleClient 也要补齐参数；Engine 已处理过的请求再次解析不会覆盖显式值。
	request, err := client.ResolveRequest(request)
	if err != nil {
		return Result{}, err
	}
	return client.clients[request.Role].Generate(ctx, request)
}

// ResolveRequest 在发请求前落实角色配置。Engine 也使用它，让日志中的预算与实际请求一致。
// 显式的阶段要求优先，例如格式修复固定关闭思考；正常生成不再在业务方法中写死参数。
func (client *RoleClient) ResolveRequest(request Request) (Request, error) {
	role := strings.ToLower(strings.TrimSpace(request.Role))
	if role == "" {
		return request, fmt.Errorf("阶段 %s 未指定工作流角色", request.Stage)
	}

	roleClient, ok := client.clients[role]
	if !ok {
		return request, fmt.Errorf("阶段 %s 使用了未配置的工作流角色 %s", request.Stage, role)
	}
	request.Role = role

	// 生效顺序：请求显式值 > YAML 角色配置 > 内置角色默认值。
	// 0 和空字符串表示尚未指定；"none" 是明确关闭思考，不能当成空值覆盖。
	if request.MaxOutputTokens == 0 {
		request.MaxOutputTokens = roleClient.config.MaxOutputTokens
	}
	if request.ReasoningEffort == "" {
		request.ReasoningEffort = roleClient.config.ReasoningEffort
	}
	return WithRoleDefaults(request), nil
}

// WithRoleDefaults 集中保存默认预算。测试生成器也走同一默认值，避免生产和测试分叉。
func WithRoleDefaults(request Request) Request {
	// 只为未指定的输出上限兜底。这是单次模型输出的资源边界，不是章节字数目标。
	// Commit 要输出状态等结构化内容，给它较大余量；失败时不自动无限放大预算。
	if request.MaxOutputTokens == 0 {
		switch request.Role {
		case RoleArchitect:
			request.MaxOutputTokens = 16000
		case RolePlanner, RoleDirector, RoleEditor:
			request.MaxOutputTokens = 6000
		case RoleWriter:
			request.MaxOutputTokens = 12000
		case RoleCommit:
			request.MaxOutputTokens = 24000
		}
	}

	// Writer 与 Commit 默认关闭思考；角色配置仍可显式覆盖这个默认选择。
	if request.ReasoningEffort == "" {
		request.ReasoningEffort = "low"
		if request.Role == RoleWriter || request.Role == RoleCommit {
			request.ReasoningEffort = "none"
		}
	}
	return request
}

// ValidateGenerationSettings 检查本地可识别的参数，不承诺每个供应商都支持所有强度。
func ValidateGenerationSettings(config Config) error {
	// 内部 Config 用 0 表示省略，允许后续补默认值；YAML 显式写 0 由配置层拒绝。
	if config.MaxOutputTokens < 0 {
		return fmt.Errorf("max_output_tokens 不能为负数")
	}

	// 空字符串留给默认值解析，none 则会作为显式关闭指令发送给模型。
	switch config.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return nil
	default:
		return fmt.Errorf("无效 reasoning_effort: %s", config.ReasoningEffort)
	}
}
