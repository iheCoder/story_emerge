// Package prompts 集中保存所有逻辑角色的中文工作说明。
// Prompt 独立成 Markdown，使非 Go 开发者也能直接审阅写作方法。
package prompts

import "embed"

//go:embed templates/*.md schemas/*.json
var files embed.FS

func Template(name string) (string, error) {
	content, err := files.ReadFile("templates/" + name + ".md")
	return string(content), err
}

func Schema(name string) ([]byte, error) {
	return files.ReadFile("schemas/" + name + ".json")
}
