// Package llm 提供一层很薄的 Responses API 客户端。
// OpenAI 与 DeepSeek 使用同一协议，供应商差异由上层 YAML 配置解析后传入。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxErrorBodyBytes 限制错误响应写入错误信息的大小。
// 模型网关可能返回很长的 HTML/调试文本；截断它既保护日志可读性，也避免异常响应占用过多内存。
const maxErrorBodyBytes = 8 << 10

// Config 描述一个角色调用模型所需的连接信息与生成参数默认值。
// APIKey 只在内存中使用，不会被序列化到小说项目文件。
type Config struct {
	Provider        string        // 供应商标识，用于日志和诊断。
	Model           string        // 实际请求使用的模型名。
	Endpoint        string        // Responses API 端点，可被本地代理覆盖。
	APIKey          string        // 仅内存持有的密钥，不写入项目文件。
	Timeout         time.Duration // 单次 HTTP 请求超时。
	ReasoningEffort string        // 角色默认推理强度，空值使用角色默认值。
	MaxOutputTokens int           // 角色输出上限，0 使用角色默认值。
}

// Request 是业务阶段对模型客户端的最小请求契约。
// Instructions 约束模型角色，Input 携带本阶段上下文；Schema 非空时要求模型返回严格 JSON。
type Request struct {
	Stage           string         // 工作流阶段名，用于错误、进度和用量记录。
	Role            string         // 工作流角色名，用于选择该角色绑定的模型客户端。
	Instructions    string         // 角色级系统指令。
	Input           string         // 本阶段结构化上下文或正文输入。
	SchemaName      string         // JSON Schema 名称；Schema 为空时不发送。
	Schema          map[string]any // 严格结构化输出契约。
	MaxOutputTokens int            // 单次输出上限；0 留给 RoleClient 补角色配置或默认值。
	ReasoningEffort string         // 空值继承角色配置；none 显式关闭思考，优先于角色配置。
	Temperature     *float64       // 可选采样温度；nil 表示交给服务端默认。
}

// RetryInfo 描述一次逻辑模型调用内部发生的真实 HTTP 尝试。
// AttemptCount 包含首次请求；RetryCount 只计算已经实际发起的额外尝试，不把尚未执行的退避计划算作 retry。
type RetryInfo struct {
	AttemptCount int
	RetryCount   int
	Recovered    bool
	RetryReason  string
}

// Result 携带模型返回的文本与元数据；响应不完整时也可能与错误一起返回，供诊断使用。
// 调用方必须先检查错误，不能因 Text 非空就把不完整内容当成成功结果提交。
// Duration 只用于观测，不参与故事决策；Retry 区分一次逻辑调用内部发生的真实网络重试。
type Result struct {
	Text       string        // 拼接后的 output_text。
	Model      string        // 服务端实际采用的模型。
	ResponseID string        // 供应商响应 ID，便于追踪问题。
	Usage      Usage         // token 统计。
	Duration   time.Duration // 最终一次 HTTP attempt 从发起请求到解码完成的耗时。
	Retry      RetryInfo     // 物理 attempt/retry 信息；非 Client 测试替身可以保持零值。
}

// Usage 对齐 Responses API 的 token 统计，便于在 usage.jsonl 中审计成本。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

// Generator 是 Engine 依赖的窄接口。
// 生产环境使用 RoleClient 路由到各角色的 Client，测试可注入固定结果，避免依赖网络。
type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

// Client 将 OpenAI/DeepSeek 的兼容 Responses API 封装成 Generator。
// 供应商差异在配置层收敛，业务层只处理“生成文本/JSON”这一个抽象动作。
type Client struct {
	config Config
	http   *http.Client
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
func (client *Client) Generate(ctx context.Context, request Request) (result Result, err error) {
	// 错误体可能由网关回显请求信息；错误进入页面和持久化日志前，遮蔽本角色密钥。
	// 保留错误链，使 context 取消等原有判断仍然有效。
	defer func() {
		if err != nil && strings.Contains(err.Error(), client.config.APIKey) {
			err = redactedError{cause: err, message: strings.ReplaceAll(err.Error(), client.config.APIKey, "[REDACTED]")}
		}
	}()
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

type redactedError struct {
	cause   error
	message string
}

func (err redactedError) Error() string { return err.message }
func (err redactedError) Unwrap() error { return err.cause }

// buildRequest 将业务请求编码成 Responses API JSON，不执行网络 IO。
func (client *Client) buildRequest(request Request) ([]byte, error) {
	// 角色配置与阶段显式参数已经由上层解析完毕，此处只把最终请求翻译为协议字段。
	// 不再次读取 Config 中的生成默认值，避免覆盖格式修复等任务的显式要求。
	payload := responseRequest{
		Model:           client.config.Model,
		Instructions:    request.Instructions,
		Input:           request.Input,
		MaxOutputTokens: request.MaxOutputTokens,
		Store:           false,
	}

	// 可选字段按最终请求决定是否发送；none 必须照常发送，它表示关闭而非省略推理设置。
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

// sendWithRetry 对可恢复故障做最多三次真实 HTTP attempt，并把物理重试事实带回上层 Observation。
func (client *Client) sendWithRetry(ctx context.Context, stage string, body []byte) (Result, error) {
	// 只重试网络瞬断、429 和 5xx；业务错误、JSON 解码错误和上下文取消不重试。
	// RetryCount 表示真正发出的额外请求，因此最后一次失败后不会再做没有下一次请求的退避等待。
	var lastErr error
	var lastResult Result
	var lastRetryReason string
	for attempt := 1; attempt <= 3; attempt++ {
		result, retryReason, err := client.sendOnce(ctx, stage, body)
		if retryReason != "" {
			lastRetryReason = retryReason
		}
		result.Retry = RetryInfo{
			AttemptCount: attempt,
			RetryCount:   attempt - 1,
			Recovered:    err == nil && attempt > 1,
			RetryReason:  lastRetryReason,
		}

		// 成功或不可重试错误都立即返回；若此前已经重试，RetryInfo 仍保留恢复/失败路径。
		if err == nil || retryReason == "" {
			return result, err
		}
		lastErr = err
		lastResult = result

		// 第三次已经是最后一个物理 attempt，失败后直接返回，不能再多等待一个不存在的第四次请求。
		if attempt == 3 {
			return lastResult, fmt.Errorf("阶段 %s 重试后仍失败: %w", stage, lastErr)
		}

		// 对可恢复错误进行递增退避；上下文取消会立即打断等待。
		if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
			return lastResult, err
		}
	}
	return lastResult, fmt.Errorf("阶段 %s 重试后仍失败: %w", stage, lastErr)
}

// sendOnce 发送单次 HTTP 请求，并返回可恢复错误的稳定分类；空分类表示本次错误不应重试。
func (client *Client) sendOnce(ctx context.Context, stage string, body []byte) (Result, string, error) {
	// 每次尝试都新建带 context 的请求，使用户 Ctrl-C 或上层超时能立即中断 HTTP。
	// retryReason 只表达 network/http_429/http_5xx，不把供应商错误正文直接变成高基数字段。
	started := time.Now()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, "", fmt.Errorf("创建 %s 请求失败: %w", stage, err)
	}

	// 附加鉴权和协议头；密钥只存在于内存中的 Header。
	httpRequest.Header.Set("Authorization", "Bearer "+client.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	// 发送请求并交给统一响应解码器分类。
	response, err := client.http.Do(httpRequest)
	if err != nil {
		reason := ""
		if isTransientNetworkError(err) {
			reason = "network"
		}
		return Result{}, reason, fmt.Errorf("调用 %s 失败: %w", stage, err)
	}
	defer response.Body.Close()
	// Do 返回时可能仅收到响应头。完整耗时必须包含随后读取和解码响应体的时间，
	// 否则长文本生成几十秒也会在日志里被错误记成几百毫秒。
	result, retryReason, err := client.decodeResponse(stage, response)
	result.Duration = time.Since(started)
	return result, retryReason, err
}

// decodeResponse 把 HTTP 响应转换为 Result 或带阶段上下文的错误，并分类是否值得重试。
func (client *Client) decodeResponse(stage string, response *http.Response) (Result, string, error) {
	// 先按 HTTP 状态分类，再解析成功响应；错误体只读取有限字节，避免把网关噪声带入日志。
	// 即使 HTTP 200，只有 completed 且包含可见 output_text 才算成功。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		err := fmt.Errorf("阶段 %s 返回 HTTP %d: %s", stage, response.StatusCode, strings.TrimSpace(string(body)))
		reason := ""
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			reason = "http_429"
		case response.StatusCode >= 500:
			reason = "http_5xx"
		}
		return Result{}, reason, err
	}

	// 解码成功响应，并提取可见文本。
	var payload responsePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Result{}, "", fmt.Errorf("解析 %s 响应失败: %w", stage, err)
	}

	// 检查业务完成状态，防止把 incomplete/tool-call 当作正文。
	text := extractOutputText(payload.Output)
	result := Result{
		Text: text, Model: payload.Model, ResponseID: payload.ID,
		Usage: payload.Usage.toUsage(),
	}
	if payload.Status != "completed" || strings.TrimSpace(text) == "" {
		// 连同错误保留供应商已返回的证据，供运行日志和 Observation 记录用量、响应 ID 与部分文本。
		return result, "", responseStatusError(stage, payload)
	}
	return result, "", nil
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
