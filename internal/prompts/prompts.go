// Package prompts 集中保存所有逻辑角色的中文工作说明。
// Prompt 独立成 Markdown，使非 Go 开发者也能直接审阅写作方法。
package prompts

import "embed"

//go:embed templates/*.md schemas/*.json
var files embed.FS

// Template 读取一个逻辑角色的 Markdown 指令。
// 模板随二进制嵌入，发布时不依赖当前工作目录，也不需要额外模板服务。
func Template(name string) (string, error) {
	// name 由 workflow 的固定阶段传入；这里保留底层错误，让缺失模板能在启动/调用时定位。
	content, err := files.ReadFile("templates/" + name + ".md")
	return string(content), err
}

// Schema 读取结构化输出契约。Schema 的存在由 generateJSON 统一检查，
// 模型返回无法解码时会进入一次格式修复而不是静默丢弃。
func Schema(name string) ([]byte, error) {
	return files.ReadFile("schemas/" + name + ".json")
}
