package llm

import (
	"fmt"
	"strings"
)

type responseRequest struct {
	Model           string           `json:"model"`
	Instructions    string           `json:"instructions"`
	Input           string           `json:"input"`
	MaxOutputTokens int              `json:"max_output_tokens"`
	Store           bool             `json:"store"`
	Reasoning       *reasoningConfig `json:"reasoning,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	Text            *textConfig      `json:"text,omitempty"`
}

type reasoningConfig struct {
	Effort string `json:"effort"`
}

type textConfig struct {
	Format jsonSchemaFormat `json:"format"`
}

type jsonSchemaFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

func structuredTextConfig(name string, schema map[string]any) *textConfig {
	return &textConfig{Format: jsonSchemaFormat{
		Type: "json_schema", Name: name, Strict: true, Schema: schema,
	}}
}

type responsePayload struct {
	ID                string             `json:"id"`
	Status            string             `json:"status"`
	Model             string             `json:"model"`
	Output            []responseOutput   `json:"output"`
	Usage             responseUsage      `json:"usage"`
	Error             *responseError     `json:"error"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details"`
}

type responseOutput struct {
	Type    string            `json:"type"`
	Content []responseContent `json:"content"`
}

type responseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseUsage struct {
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	TotalTokens       int `json:"total_tokens"`
	InputTokenDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type incompleteDetails struct {
	Reason string `json:"reason"`
}

func extractOutputText(outputs []responseOutput) string {
	var parts []string
	for _, output := range outputs {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func responseStatusError(stage string, payload responsePayload) error {
	if payload.Error != nil {
		return fmt.Errorf("阶段 %s 失败 [%s]: %s", stage, payload.Error.Code, payload.Error.Message)
	}
	if payload.IncompleteDetails != nil {
		return fmt.Errorf("阶段 %s 输出不完整: %s", stage, payload.IncompleteDetails.Reason)
	}
	return fmt.Errorf("阶段 %s 状态异常: %s", stage, payload.Status)
}

func (usage responseUsage) toUsage() Usage {
	return Usage{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		TotalTokens: usage.TotalTokens, CachedTokens: usage.InputTokenDetails.CachedTokens,
	}
}
