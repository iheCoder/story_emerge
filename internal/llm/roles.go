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
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			return nil, fmt.Errorf("角色名称不能为空")
		}
		if _, exists := clients[role]; exists {
			return nil, fmt.Errorf("角色模型配置重复: %s", role)
		}
		client, err := NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("角色 %s 的模型配置无效: %w", role, err)
		}
		clients[role] = client
	}

	return &RoleClient{clients: clients}, nil
}

// Generate 根据请求中的角色选择客户端；空角色或未配置角色都直接失败。
func (client *RoleClient) Generate(ctx context.Context, request Request) (Result, error) {
	role := strings.ToLower(strings.TrimSpace(request.Role))
	if role == "" {
		return Result{}, fmt.Errorf("阶段 %s 未指定工作流角色", request.Stage)
	}

	roleClient, ok := client.clients[role]
	if !ok {
		return Result{}, fmt.Errorf("阶段 %s 使用了未配置的工作流角色 %s", request.Stage, role)
	}
	return roleClient.Generate(ctx, request)
}
