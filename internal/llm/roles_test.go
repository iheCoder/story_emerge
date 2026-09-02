package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRoleClientDispatchesEachRequestToItsConfiguredModel(t *testing.T) {
	// 场景：Writer 与 Planner 分别绑定不同模型，并通过同一个 RoleClient 发起请求。
	// 预期：路由依据 Role 字段选择客户端，实际请求体中的 model 不会被其他角色覆盖。
	client, err := NewRoleClient(map[string]Config{
		RoleWriter:  {Provider: "test", Model: "writer-model", Endpoint: "https://writer.test", APIKey: "writer-key"},
		RolePlanner: {Provider: "test", Model: "planner-model", Endpoint: "https://planner.test", APIKey: "planner-key"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for role, expectedModel := range map[string]string{
		RoleWriter:  "writer-model",
		RolePlanner: "planner-model",
	} {
		role := role
		expectedModel := expectedModel
		client.clients[role].http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["model"] != expectedModel {
				t.Fatalf("角色 %s 使用了错误模型: got=%v want=%s", role, payload["model"], expectedModel)
			}
			return jsonHTTPResponse(http.StatusOK, successResponse), nil
		})

		if _, err := client.Generate(context.Background(), Request{Role: role, Stage: role, Input: "input", MaxOutputTokens: 1}); err != nil {
			t.Fatalf("角色 %s 调用失败: %v", role, err)
		}
	}
}

func TestRoleClientRejectsMissingRole(t *testing.T) {
	// 场景：工作流请求没有角色标识，无法安全选择模型。
	// 预期：在网络请求前失败，避免错误地把请求发送给某个默认角色。
	client, err := NewRoleClient(map[string]Config{
		RoleWriter: {Provider: "test", Model: "writer-model", Endpoint: "https://writer.test", APIKey: "key"},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Generate(context.Background(), Request{Stage: "chapter_001_write", Input: "input", MaxOutputTokens: 1})
	if err == nil || !strings.Contains(err.Error(), "未指定工作流角色") {
		t.Fatalf("未拒绝空角色请求: %v", err)
	}
}
