// Package llm 提供一层很薄的 Responses API 客户端。
// OpenAI 与 DeepSeek 使用同一协议，差异只保留在端点、密钥环境变量和默认模型中。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// maxErrorBodyBytes 限制错误响应写入错误信息的大小。
// 模型网关可能返回很长的 HTML/调试文本；截断它既保护日志可读性，也避免异常响应占用过多内存。
const maxErrorBodyBytes = 8 << 10

// Config 描述一次模型调用所需的连接信息。
// APIKey 只在内存中使用，不会被序列化到小说项目文件；Provider/Model 会写入 project.json 以便恢复。
type Config struct {
	Provider string        // 供应商标识，用于日志和默认配置选择。
	Model    string        // 实际请求使用的模型名。
	Endpoint string        // Responses API 端点，可被本地代理覆盖。
	APIKey   string        // 仅内存持有的密钥，不写入项目文件。
	Timeout  time.Duration // 单次 HTTP 请求超时。
}

// Request 是业务阶段对模型客户端的最小请求契约。
// Instructions 约束模型角色，Input 携带本阶段上下文；Schema 非空时要求模型返回严格 JSON。
type Request struct {
	Stage           string         // 工作流阶段名，用于错误、进度和用量记录。
	Instructions    string         // 角色级系统指令。
	Input           string         // 本阶段结构化上下文或正文输入。
	SchemaName      string         // JSON Schema 名称；Schema 为空时不发送。
	Schema          map[string]any // 严格结构化输出契约。
	MaxOutputTokens int            // 单次响应的成本/截断护栏。
	ReasoningEffort string         // 可选推理强度。
	Temperature     *float64       // 可选采样温度；nil 表示交给服务端默认。
}

// Result 是模型调用成功后的统一结果，供工作流保存正文、解析结构化数据和记录用量。
// Duration 只用于观测，不参与故事决策。
type Result struct {
	Text       string        // 拼接后的 output_text。
	Model      string        // 服务端实际采用的模型。
	ResponseID string        // 供应商响应 ID，便于追踪问题。
	Usage      Usage         // token 统计。
	Duration   time.Duration // 从发起请求到解码完成的耗时。
}

// Usage 对齐 Responses API 的 token 统计，便于在 usage.jsonl 中审计成本。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

// Generator 是 Engine 依赖的窄接口。
// 生产环境使用 Client，测试则可注入按阶段返回固定结果的 fake，避免测试依赖网络。
type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

// Client 将 OpenAI/DeepSeek 的兼容 Responses API 封装成 Generator。
// 供应商差异在配置层收敛，业务层只处理“生成文本/JSON”这一个抽象动作。
type Client struct {
	config Config
	http   *http.Client
}

// ConfigFromEnv 集中处理提供商差异，业务代码无需感知具体平台。
func ConfigFromEnv(provider, model string) (Config, error) {
	// 这里是唯一的提供商分流点：调用方不需要知道密钥变量名和端点规则。
	// provider 做大小写和空白归一化，但 model 保留调用方的精确值以便严格校验。
	// 归一化并分派提供商。
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek":
		return deepSeekConfig(model)
	case "openai":
		return openAIConfig(model)
	default:
		return Config{}, fmt.Errorf("不支持的模型提供商: %s", provider)
	}
}

// deepSeekConfig 生成项目约定的 DeepSeek Flash 配置。
func deepSeekConfig(model string) (Config, error) {
	// 项目锁定 DeepSeek Flash：成本和延迟更适合长篇试跑，
	// 同时避免模型能力差异导致提示词调优结果不可复现。
	// 未指定模型时使用默认的 Flash 模型。
	if model == "" {
		model = "deepseek-v4-flash"
	}

	// 显式指定非 Flash 模型时立即拒绝，避免悄悄扩大成本和能力变量。
	if model != "deepseek-v4-flash" {
		return Config{}, fmt.Errorf("DeepSeek 只允许使用 deepseek-v4-flash")
	}

	// 读取密钥并组装最终连接配置。
	return configWithKey("deepseek", model, "https://api.deepseek.com/responses", "DEEPSEEK_API_KEY")
}

// openAIConfig 生成 OpenAI Responses API 配置，支持调用方覆盖默认模型。
func openAIConfig(model string) (Config, error) {
	// OpenAI 默认模型只在未指定时生效；显式传入的模型交由服务端验证。
	// 未指定模型时使用 OpenAI 默认配置。
	if model == "" {
		model = "gpt-5.6-luna"
	}

	// 读取密钥并保留调用方显式指定的模型名。
	return configWithKey("openai", model, "https://api.openai.com/v1/responses", "OPENAI_API_KEY")
}

// configWithKey 从指定环境变量读取密钥，并应用可选的本地代理端点。
func configWithKey(provider, model, endpoint, keyName string) (Config, error) {
	// 密钥从环境变量读取，既避免落盘泄露，也让同一项目可以在不同机器切换账号。
	// STORY_EMERGE_BASE_URL 仅用于本地代理/测试替换端点，仍自动补上 Responses API 路径。
	// 从环境读取密钥；缺少密钥时在启动阶段失败。
	key := strings.TrimSpace(os.Getenv(keyName))
	if key == "" {
		return Config{}, fmt.Errorf("缺少环境变量 %s", keyName)
	}

	// 应用可选的代理地址。
	if override := strings.TrimSpace(os.Getenv("STORY_EMERGE_BASE_URL")); override != "" {
		endpoint = strings.TrimRight(override, "/") + "/responses"
	}
	return Config{Provider: provider, Model: model, Endpoint: endpoint, APIKey: key}, nil
}

// NewClient 校验配置并创建带默认超时的 HTTP 客户端。
func NewClient(config Config) (*Client, error) {
	// 在启动阶段尽早拒绝不完整配置，避免跑到某个章节才因空端点或空模型失败。
	// 默认十分钟超时覆盖长章节生成；调用方仍可用 context 提前取消。
	// 先验证连接配置的必填项。
	if config.APIKey == "" || config.Endpoint == "" || config.Model == "" {
		return nil, fmt.Errorf("模型客户端配置不完整")
	}

	// 未指定超时时应用长文本生成的默认窗口。
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	// 创建带配置快照的 HTTP 客户端，后续请求不再依赖外部可变配置。
	return &Client{config: config, http: &http.Client{Timeout: config.Timeout}}, nil
}

// Generate 执行一个完整的模型请求，并返回统一文本、用量和响应元数据。
func (client *Client) Generate(ctx context.Context, request Request) (Result, error) {
	// 所有阶段从这里进入网络层：先校验输出预算，再构造协议负载，最后执行有限重试。
	// 输出 token 上限是成本护栏，缺失时宁可立即失败也不发送无限制请求。
	// 检查输出预算。
	if request.MaxOutputTokens < 1 {
		return Result{}, fmt.Errorf("阶段 %s 未设置输出 token 上限", request.Stage)
	}

	// 将业务请求转换为供应商协议负载。
	body, err := client.buildRequest(request)
	if err != nil {
		return Result{}, err
	}

	// 执行带有限重试的网络调用。
	return client.sendWithRetry(ctx, request.Stage, body)
}

// buildRequest 将业务请求编码成 Responses API JSON，不执行网络 IO。
func (client *Client) buildRequest(request Request) ([]byte, error) {
	// Responses API 的公共字段在此集中组装；可选推理、温度和 JSON Schema
	// 只在调用阶段明确要求时写入，避免不同阶段共享隐式参数。
	// 填充所有阶段共有的请求字段。
	payload := responseRequest{
		Model:           client.config.Model,
		Instructions:    request.Instructions,
		Input:           request.Input,
		MaxOutputTokens: request.MaxOutputTokens,
		Store:           false,
	}

	// 仅附加调用方明确要求的可选参数，避免阶段之间共享隐式配置。
	if request.ReasoningEffort != "" {
		payload.Reasoning = &reasoningConfig{Effort: request.ReasoningEffort}
	}
	if request.Temperature != nil {
		payload.Temperature = request.Temperature
	}
	if request.Schema != nil {
		payload.Text = structuredTextConfig(request.SchemaName, request.Schema)
	}
	return json.Marshal(payload)
}

// sendWithRetry 对可恢复故障做最多三次线性退避重试。
func (client *Client) sendWithRetry(ctx context.Context, stage string, body []byte) (Result, error) {
	// 只重试网络瞬断、429 和 5xx；业务错误、JSON 解码错误和上下文取消不重试，
	// 防止坏提示词在调用预算内反复消耗额度。退避时间按第几次尝试递增。
	// 逐次发起请求，并根据 retry 标志判断是否继续。
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, retry, err := client.sendOnce(ctx, stage, body)
		if err == nil || !retry {
			return result, err
		}
		lastErr = err

		// 对可恢复错误进行递增退避；上下文取消会立即打断等待。
		if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
			return Result{}, err
		}
	}
	return Result{}, fmt.Errorf("阶段 %s 重试后仍失败: %w", stage, lastErr)
}

// sendOnce 发送单次 HTTP 请求，并返回该错误是否值得由上层重试。
func (client *Client) sendOnce(ctx context.Context, stage string, body []byte) (Result, bool, error) {
	// 每次尝试都新建带 context 的请求，使用户 Ctrl-C 或上层超时能立即中断 HTTP。
	// 返回值中的 retry 标志由响应分类决定，供上层保持“有限且可解释”的重试策略。
	// 创建绑定 context 的请求。
	started := time.Now()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, false, fmt.Errorf("创建 %s 请求失败: %w", stage, err)
	}

	// 附加鉴权和协议头；密钥只存在于内存中的 Header。
	httpRequest.Header.Set("Authorization", "Bearer "+client.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	// 发送请求并交给统一响应解码器分类。
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return Result{}, isTransientNetworkError(err), fmt.Errorf("调用 %s 失败: %w", stage, err)
	}
	defer response.Body.Close()
	return client.decodeResponse(stage, response, time.Since(started))
}

// decodeResponse 把 HTTP 响应转换为 Result 或带阶段上下文的错误。
func (client *Client) decodeResponse(stage string, response *http.Response, duration time.Duration) (Result, bool, error) {
	// 先按 HTTP 状态分类，再解析成功响应；错误体只读取有限字节，避免把网关噪声带入日志。
	// 即使 HTTP 200，只有 completed 且包含可见 output_text 才算成功，
	// 因为 Responses API 可能返回 incomplete 或 tool-call 等非正文结果。
	// 先处理 HTTP 层错误；只有 429/5xx 允许上层重试。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		err := fmt.Errorf("阶段 %s 返回 HTTP %d: %s", stage, response.StatusCode, strings.TrimSpace(string(body)))
		return Result{}, response.StatusCode == 429 || response.StatusCode >= 500, err
	}

	// 解码成功响应，并提取可见文本。
	var payload responsePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Result{}, false, fmt.Errorf("解析 %s 响应失败: %w", stage, err)
	}

	// 检查业务完成状态，防止把 incomplete/tool-call 当作正文。
	text := extractOutputText(payload.Output)
	if payload.Status != "completed" || strings.TrimSpace(text) == "" {
		return Result{}, false, responseStatusError(stage, payload)
	}
	return Result{
		Text: text, Model: payload.Model, ResponseID: payload.ID,
		Usage: payload.Usage.toUsage(), Duration: duration,
	}, false, nil
}

// waitForRetry 可被 context 中断的退避等待。
func waitForRetry(ctx context.Context, delay time.Duration) error {
	// 用 timer 等待而不是阻塞 sleep，这样取消上下文时可以立刻结束等待。
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isTransientNetworkError 判断网络错误是否不是主动取消或超时。
func isTransientNetworkError(err error) bool {
	// 普通网络错误通常可重试，但用户取消和 deadline 已经表达了明确意图，必须原样透传。
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
