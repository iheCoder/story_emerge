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

const maxErrorBodyBytes = 8 << 10

type Config struct {
	Provider string
	Model    string
	Endpoint string
	APIKey   string
	Timeout  time.Duration
}

type Request struct {
	Stage           string
	Instructions    string
	Input           string
	SchemaName      string
	Schema          map[string]any
	MaxOutputTokens int
	ReasoningEffort string
	Temperature     *float64
}

type Result struct {
	Text       string
	Model      string
	ResponseID string
	Usage      Usage
	Duration   time.Duration
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

type Client struct {
	config Config
	http   *http.Client
}

// ConfigFromEnv 集中处理提供商差异，业务代码无需感知具体平台。
func ConfigFromEnv(provider, model string) (Config, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek":
		return deepSeekConfig(model)
	case "openai":
		return openAIConfig(model)
	default:
		return Config{}, fmt.Errorf("不支持的模型提供商: %s", provider)
	}
}

func deepSeekConfig(model string) (Config, error) {
	if model == "" {
		model = "deepseek-v4-flash"
	}
	if model != "deepseek-v4-flash" {
		return Config{}, fmt.Errorf("V1 的 DeepSeek 只允许使用 deepseek-v4-flash")
	}
	return configWithKey("deepseek", model, "https://api.deepseek.com/responses", "DEEPSEEK_API_KEY")
}

func openAIConfig(model string) (Config, error) {
	if model == "" {
		model = "gpt-5.6-luna"
	}
	return configWithKey("openai", model, "https://api.openai.com/v1/responses", "OPENAI_API_KEY")
}

func configWithKey(provider, model, endpoint, keyName string) (Config, error) {
	key := strings.TrimSpace(os.Getenv(keyName))
	if key == "" {
		return Config{}, fmt.Errorf("缺少环境变量 %s", keyName)
	}
	if override := strings.TrimSpace(os.Getenv("STORY_EMERGE_BASE_URL")); override != "" {
		endpoint = strings.TrimRight(override, "/") + "/responses"
	}
	return Config{Provider: provider, Model: model, Endpoint: endpoint, APIKey: key}, nil
}

func NewClient(config Config) (*Client, error) {
	if config.APIKey == "" || config.Endpoint == "" || config.Model == "" {
		return nil, fmt.Errorf("模型客户端配置不完整")
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}
	return &Client{config: config, http: &http.Client{Timeout: config.Timeout}}, nil
}

func (client *Client) Generate(ctx context.Context, request Request) (Result, error) {
	if request.MaxOutputTokens < 1 {
		return Result{}, fmt.Errorf("阶段 %s 未设置输出 token 上限", request.Stage)
	}
	body, err := client.buildRequest(request)
	if err != nil {
		return Result{}, err
	}
	return client.sendWithRetry(ctx, request.Stage, body)
}

func (client *Client) buildRequest(request Request) ([]byte, error) {
	payload := responseRequest{
		Model:           client.config.Model,
		Instructions:    request.Instructions,
		Input:           request.Input,
		MaxOutputTokens: request.MaxOutputTokens,
		Store:           false,
	}
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

func (client *Client) sendWithRetry(ctx context.Context, stage string, body []byte) (Result, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, retry, err := client.sendOnce(ctx, stage, body)
		if err == nil || !retry {
			return result, err
		}
		lastErr = err
		if err := waitForRetry(ctx, time.Duration(attempt)*time.Second); err != nil {
			return Result{}, err
		}
	}
	return Result{}, fmt.Errorf("阶段 %s 重试后仍失败: %w", stage, lastErr)
}

func (client *Client) sendOnce(ctx context.Context, stage string, body []byte) (Result, bool, error) {
	started := time.Now()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, false, fmt.Errorf("创建 %s 请求失败: %w", stage, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+client.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return Result{}, isTransientNetworkError(err), fmt.Errorf("调用 %s 失败: %w", stage, err)
	}
	defer response.Body.Close()
	return client.decodeResponse(stage, response, time.Since(started))
}

func (client *Client) decodeResponse(stage string, response *http.Response, duration time.Duration) (Result, bool, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		err := fmt.Errorf("阶段 %s 返回 HTTP %d: %s", stage, response.StatusCode, strings.TrimSpace(string(body)))
		return Result{}, response.StatusCode == 429 || response.StatusCode >= 500, err
	}
	var payload responsePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Result{}, false, fmt.Errorf("解析 %s 响应失败: %w", stage, err)
	}
	text := extractOutputText(payload.Output)
	if payload.Status != "completed" || strings.TrimSpace(text) == "" {
		return Result{}, false, responseStatusError(stage, payload)
	}
	return Result{
		Text: text, Model: payload.Model, ResponseID: payload.ID,
		Usage: payload.Usage.toUsage(), Duration: duration,
	}, false, nil
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientNetworkError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
