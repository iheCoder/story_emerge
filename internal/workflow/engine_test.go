package workflow

import (
	"errors"
	"strings"
	"testing"

	"story_emerge/internal/llm"
)

func TestJSONRepairRequestIncludesSchemaTypeFailure(t *testing.T) {
	// 场景：模型返回合法 JSON，但把 Schema 要求的字符串数组写成字符串。
	// 预期：修复请求包含精确类型错误，不能只要求修复括号和逗号。
	request := llm.Request{Stage: "chapter_004_reader", ReasoningEffort: "low"}
	decodeErr := errors.New("cannot unmarshal string into received_payoff of type []string")

	repaired := jsonRepairRequest(request, `{"received_payoff":"回报"}`, decodeErr)

	if repaired.Stage != "chapter_004_reader_format_repair" || repaired.ReasoningEffort != "none" {
		t.Fatalf("修复阶段配置错误: %#v", repaired)
	}
	if !strings.Contains(repaired.Input, decodeErr.Error()) || !strings.Contains(repaired.Input, `"received_payoff":"回报"`) {
		t.Fatalf("修复请求缺少解析错误或原答案: %s", repaired.Input)
	}
}
