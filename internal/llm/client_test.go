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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestGenerateBuildsStructuredRequestAndCollectsAllText(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assertRequest(t, request)
		return jsonHTTPResponse(http.StatusOK, successResponse), nil
	})
	client, err := NewClient(Config{Provider: "test", Model: "model", Endpoint: "https://example.test", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = transport
	result, err := client.Generate(context.Background(), Request{
		Stage: "test", Instructions: "规则", Input: "输入", SchemaName: "shape",
		Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100, ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "第一段\n第二段" || result.Usage.CachedTokens != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestGenerateDoesNotLeakAPIKeyInHTTPError(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusBadRequest, "bad request"), nil
	})
	client, _ := NewClient(Config{Provider: "test", Model: "model", Endpoint: "https://example.test", APIKey: "top-secret"})
	client.http.Transport = transport
	_, err := client.Generate(context.Background(), Request{Stage: "test", Input: "x", MaxOutputTokens: 1})
	if err == nil || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("expected redacted HTTP error, got %v", err)
	}
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func assertRequest(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("missing authorization header")
	}
	var payload map[string]any
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	text, ok := payload["text"].(map[string]any)
	if !ok || text["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("missing JSON schema format: %#v", payload)
	}
}

const successResponse = `{
  "id":"resp_1","status":"completed","model":"model",
  "output":[
    {"type":"reasoning","content":[]},
    {"type":"message","content":[{"type":"output_text","text":"第一段"}]},
    {"type":"message","content":[{"type":"output_text","text":"第二段"}]}
  ],
  "usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":2}}
}`
