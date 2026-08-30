package webapp

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/workflow"
)

const previewChapterCount = 3

// CreateRequest 是 Web 产品允许读者表达的全部创作输入。
// 具体章数、人物数量和叙事结构由总导演在篇幅边界内决定。
type CreateRequest struct {
	Idea   string `json:"idea"`
	Length string `json:"length"`
}

// Runtime 把 HTTP 生命周期与小说工作流隔开，测试可以在不调用真实模型的情况下
// 验证异步创建、逐章可读和续写行为。
type Runtime interface {
	CreatePreview(context.Context, string, CreateRequest, workflow.Reporter) error
	Continue(context.Context, string, int, workflow.Reporter) error
}

// WorkflowRuntime 使用现有 Engine 实现 Web 产品的三章试读与续写契约。
type WorkflowRuntime struct {
	config llm.Config
}

// NewWorkflowRuntime 在服务器启动时验证模型配置，避免用户提交灵感后才发现缺少密钥。
func NewWorkflowRuntime(provider, model string) (*WorkflowRuntime, error) {
	config, err := llm.ConfigFromEnv(provider, model)
	if err != nil {
		return nil, err
	}
	return &WorkflowRuntime{config: config}, nil
}

// CreatePreview 初始化故事，再严格限制本次运行只提交前三章。
func (runtime *WorkflowRuntime) CreatePreview(
	ctx context.Context,
	root string,
	request CreateRequest,
	reporter workflow.Reporter,
) error {
	project := runtime.newProject(root, request)
	engine, err := runtime.newEngine(store.New(root), project.MaxCalls, reporter)
	if err != nil {
		return err
	}
	if _, err := engine.Initialize(ctx, project); err != nil {
		return err
	}
	return engine.Run(ctx, previewChapterCount)
}

// Continue 从磁盘 HEAD 恢复项目；limit=1 表示下一章，limit=0 表示完成全书。
func (runtime *WorkflowRuntime) Continue(
	ctx context.Context,
	root string,
	limit int,
	reporter workflow.Reporter,
) error {
	files := store.New(root)
	project, err := files.LoadProject()
	if err != nil {
		return err
	}
	engine, err := runtime.newEngine(files, project.MaxCalls, reporter)
	if err != nil {
		return err
	}
	return engine.Run(ctx, limit)
}

// newProject 把产品层的篇幅档位转成总导演可决策的项目契约。
func (runtime *WorkflowRuntime) newProject(root string, request CreateRequest) story.Project {
	return story.Project{
		Version: story.FormatVersion, Name: filepath.Base(root), Idea: request.Idea,
		LengthProfile: request.Length, Provider: runtime.config.Provider, Model: runtime.config.Model,
		ChapterMinChars: 1600, ChapterMaxChars: 2200,
		MaxCalls: callBudget(request.Length), CreatedAt: time.Now().UTC(),
	}
}

// newEngine 复用工作流的预算、用量审计和事务提交能力。
func (runtime *WorkflowRuntime) newEngine(
	files *store.Store,
	maxCalls int,
	reporter workflow.Reporter,
) (*workflow.Engine, error) {
	client, err := llm.NewClient(runtime.config)
	if err != nil {
		return nil, err
	}
	return workflow.New(client, files, maxCalls, reporter)
}

// callBudget 按可能的完整故事规模提供有界预算，仍由 Engine 对每次逻辑调用统一记账。
func callBudget(length string) int {
	switch length {
	case "short":
		return 90
	case "medium":
		return 160
	case "long":
		return 260
	default:
		panic(fmt.Sprintf("unexpected length profile %q", length))
	}
}
