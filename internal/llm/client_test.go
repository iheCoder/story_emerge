package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc 把函数适配成 http.RoundTripper，使客户端测试完全脱离真实网络。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	// 测试通过闭包断言请求并返回固定响应；这比启动测试服务器更聚焦于客户端协议。
	return function(request)
}

func TestGenerateBuildsStructuredRequestAndCollectsAllText(t *testing.T) {
	// 场景：调用方要求 JSON Schema，服务端分两段返回正文。
	// 预期：请求带认证和 structured output 配置，客户端按顺序拼接全部 output_text，
	// 并保留缓存 token 统计；验证的是协议映射而非网络连通性。

	// 构造假的 HTTP 传输层，并在收到请求时检查协议字段。
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assertRequest(t, request)
		return jsonHTTPResponse(http.StatusOK, successResponse), nil
	})

	// 创建客户端并替换 Transport，保持测试完全离线。
	client, err := NewClient(Config{Provider: "test", Model: "model", Endpoint: "https://example.test", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = transport

	// 发送一个要求 JSON Schema 的结构化请求。
	result, err := client.Generate(context.Background(), Request{
		Stage: "test", Instructions: "规则", Input: "输入", SchemaName: "shape",
		Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100, ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 验证多段正文拼接和缓存 token 统计。
	if result.Text != "第一段\n第二段" || result.Usage.CachedTokens != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestGenerateDoesNotLeakAPIKeyInHTTPError(t *testing.T) {
	// 场景：网关返回 400 且错误体可能被记录。
	// 预期：调用失败但错误信息不包含 Bearer 密钥，验证敏感信息不会随诊断文本泄露。

	// 让假的传输层返回 HTTP 400。
	t.Parallel()
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusBadRequest, "bad request"), nil
	})
	client, _ := NewClient(Config{Provider: "test", Model: "model", Endpoint: "https://example.test", APIKey: "top-secret"})
	client.http.Transport = transport

	// 发起请求并检查错误信息的脱敏边界。
	_, err := client.Generate(context.Background(), Request{Stage: "test", Input: "x", MaxOutputTokens: 1})
	if err == nil || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("expected redacted HTTP error, got %v", err)
	}
}

func jsonHTTPResponse(status int, body string) *http.Response {
	// 构造最小可消费的 HTTP 响应，Body 使用 NopCloser 满足客户端关闭响应体的契约。
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func assertRequest(t *testing.T, request *http.Request) {
	// 断言测试输入的关键业务约束：鉴权正确、请求可解码、结构化阶段确实声明 json_schema。
	t.Helper()

	// 确认请求携带正确的 Bearer 鉴权。
	if request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("missing authorization header")
	}

	// 解码请求体，验证客户端发送的是合法 JSON。
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}

	// 确认结构化阶段声明了 json_schema 输出格式。
	text, ok := payload["text"].(map[string]any)
	if !ok || text["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("missing JSON schema format: %#v", payload)
	}
}

// successResponse 同时覆盖 reasoning 项和两个 message 项，防止实现只读取 output[0]。
const successResponse = `{
  "id":"resp_1","status":"completed","model":"model",
  "output":[
    {"type":"reasoning","content":[]},
    {"type":"message","content":[{"type":"output_text","text":"第一段"}]},
    {"type":"message","content":[{"type":"output_text","text":"第二段"}]}
  ],
  "usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":2}}
}`
