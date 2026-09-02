package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// validateOutputShape 补足 Go 解码不会检查 required、null 和枚举的边界。
// 只解释本项目静态 Schema 使用的结构，不判断文学内容或自动补齐缺失业务字段。
func validateOutputShape(text string, schema map[string]any) error {
	var value any
	if err := json.Unmarshal([]byte(cleanJSON(text)), &value); err != nil {
		return err
	}
	return checkShape(value, schema, "output")
}
func cleanJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
func checkShape(value any, schema map[string]any, path string) error {
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			if checkShape(value, alternative.(map[string]any), path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s 不符合任何允许的结构", path)
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("%s 应为 null", path)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 应为对象", path)
		}
		properties := schema["properties"].(map[string]any)
		for _, key := range schema["required"].([]any) {
			if _, ok := object[key.(string)]; !ok {
				return fmt.Errorf("%s 缺少必需字段 %s", path, key)
			}
		}
		for key, child := range object {
			definition, ok := properties[key]
			if !ok {
				return fmt.Errorf("%s 包含未授权字段 %s", path, key)
			}
			if err := checkShape(child, definition.(map[string]any), path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s 应为数组", path)
		}
		if limit, ok := schema["maxItems"].(float64); ok && len(array) > int(limit) {
			return fmt.Errorf("%s 超过项目数量上限", path)
		}
		if limit, ok := schema["minItems"].(float64); ok && len(array) < int(limit) {
			return fmt.Errorf("%s 缺少必要项目", path)
		}
		for i, child := range array {
			if err := checkShape(child, schema["items"].(map[string]any), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s 应为字符串", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s 应为布尔值", path)
		}
	default:
		return fmt.Errorf("%s 使用了未实现的 Schema 类型", path)
	}
	if allowed, ok := schema["enum"].([]any); ok {
		for _, option := range allowed {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("%s 使用了未允许的值", path)
	}
	return nil
}
