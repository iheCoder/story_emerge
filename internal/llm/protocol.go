package llm

import (
	"fmt"
	"strings"
)

// responseRequest 是 Responses API 的最小请求体。
// 业务层的 Request 不直接暴露 JSON 标签，避免提示词编排代码和供应商协议耦合。
type responseRequest struct {
	Model           string           `json:"model"`                 // 模型名称。
	Instructions    string           `json:"instructions"`          // 角色指令。
	Input           string           `json:"input"`                 // 业务输入。
	MaxOutputTokens int              `json:"max_output_tokens"`     // 输出预算。
	Store           bool             `json:"store"`                 // 禁止供应商持久化请求。
	Reasoning       *reasoningConfig `json:"reasoning,omitempty"`   // 可选推理设置。
	Temperature     *float64         `json:"temperature,omitempty"` // 可选采样温度。
	Text            *textConfig      `json:"text,omitempty"`        // 结构化输出设置。
}

// reasoningConfig 只在调用方请求推理时出现；省略它可减少普通写作阶段的额外开销。
type reasoningConfig struct {
	Effort string `json:"effort"` // low/none 等供应商支持的推理档位。
}

// textConfig 描述模型输出格式。结构化阶段使用 JSON Schema，正文阶段则省略该字段。
type textConfig struct {
	Format jsonSchemaFormat `json:"format"`
}

// jsonSchemaFormat 是 Responses API 对 strict JSON Schema 的包装结构。
type jsonSchemaFormat struct {
	Type   string         `json:"type"`   // 固定为 json_schema。
	Name   string         `json:"name"`   // 服务端识别用名称。
	Strict bool           `json:"strict"` // 固定开启严格模式。
	Schema map[string]any `json:"schema"` // JSON Schema 正文。
}

// structuredTextConfig 创建 strict JSON Schema 输出配置。
func structuredTextConfig(name string, schema map[string]any) *textConfig {
	// strict=true 是状态化写作的关键护栏：解析失败会触发一次修复，而不是把半截 JSON
	// 当作新的故事状态写回磁盘。
	return &textConfig{Format: jsonSchemaFormat{
		Type: "json_schema", Name: name, Strict: true, Schema: schema,
	}}
}

// responsePayload 只保留工作流真正需要的响应字段，其余供应商字段会被安全忽略。
type responsePayload struct {
	ID                string             `json:"id"`
	Status            string             `json:"status"`
	Model             string             `json:"model"`
	Output            []responseOutput   `json:"output"`
	Usage             responseUsage      `json:"usage"`
	Error             *responseError     `json:"error"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details"`
}

// responseOutput 表示 Responses API 输出数组中的一个项目；同一响应可能混有非 message 项。
type responseOutput struct {
	Type    string            `json:"type"`
	Content []responseContent `json:"content"`
}

// responseContent 是 message 内的内容片段，只有 output_text 才能作为小说文本使用。
type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// responseUsage 保留输入、输出和缓存 token，供成本审计而不影响正文逻辑。
type responseUsage struct {
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	TotalTokens       int `json:"total_tokens"`
	InputTokenDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// responseError 是服务端返回的结构化错误；没有该字段时由状态和 incomplete_details 解释失败。
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// incompleteDetails 说明 HTTP 成功但模型没有完成输出的原因，例如达到 token 上限。
type incompleteDetails struct {
	Reason string `json:"reason"`
}

// extractOutputText 从输出数组中提取所有可见 message 文本。
func extractOutputText(outputs []responseOutput) string {
	// 按供应商定义过滤 output：忽略 reasoning/tool 等项目，只拼接 message 中的文本片段。
	// 片段之间用换行连接，既保留段落边界，也兼容一次响应分多块返回正文。

	// 只保留 message 类型的输出项。
	var parts []string
	for _, output := range outputs {
		if output.Type != "message" {
			continue
		}

		// 提取其中的 output_text 片段，忽略其他内容类型。
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// responseStatusError 将响应中的 error/incomplete/status 翻译成可诊断错误。
func responseStatusError(stage string, payload responsePayload) error {
	// 将“业务失败、输出不完整、未知状态”翻译成带阶段名的中文错误，
	// 让 CLI 和 status 日志能直接定位是 Architect、Writer、Editor 或 Reader 哪一环出问题。
	// 优先报告服务端显式错误。
	if payload.Error != nil {
		return fmt.Errorf("阶段 %s 失败 [%s]: %s", stage, payload.Error.Code, payload.Error.Message)
	}

	// 其次报告 HTTP 成功但输出未完成的原因。
	if payload.IncompleteDetails != nil {
		return fmt.Errorf("阶段 %s 输出不完整: %s", stage, payload.IncompleteDetails.Reason)
	}
	return fmt.Errorf("阶段 %s 状态异常: %s", stage, payload.Status)
}

// toUsage 将协议层 token 统计转换成业务层 Usage。
func (usage responseUsage) toUsage() Usage {
	// 将协议层的嵌套缓存统计投影到业务层的扁平 Usage，避免上层依赖供应商字段布局。
	return Usage{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		TotalTokens: usage.TotalTokens, CachedTokens: usage.InputTokenDetails.CachedTokens,
	}
}
