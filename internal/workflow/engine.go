// Package workflow 实现一个显式、可恢复的小说生产状态机。
// 每个阶段都是普通 Go 方法；所谓“角色”只是职责明确的 Prompt，而不是隐式 Agent Runtime。
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"story_emerge/internal/llm"
	"story_emerge/internal/prompts"
	"story_emerge/internal/store"
)

type Event struct {
	Stage   string
	Message string
}

type Reporter func(Event)

type Engine struct {
	generator llm.Generator
	store     *store.Store
	reporter  Reporter
	maxCalls  int
	usedCalls int
}

func New(generator llm.Generator, files *store.Store, maxCalls int, reporter Reporter) (*Engine, error) {
	usedCalls, err := files.CountUsage()
	if err != nil {
		return nil, fmt.Errorf("统计既有模型调用失败: %w", err)
	}
	return &Engine{
		generator: generator, store: files, reporter: reporter,
		maxCalls: maxCalls, usedCalls: usedCalls,
	}, nil
}

func (engine *Engine) emit(stage, message string) {
	if engine.reporter != nil {
		engine.reporter(Event{Stage: stage, Message: message})
	}
}

// generate 统一执行预算检查、调用和用量落盘，避免某个新增阶段绕过成本控制。
func (engine *Engine) generate(ctx context.Context, request llm.Request, record bool) (llm.Result, error) {
	if engine.maxCalls > 0 && engine.usedCalls >= engine.maxCalls {
		return llm.Result{}, fmt.Errorf("已达到模型调用上限 %d", engine.maxCalls)
	}
	engine.usedCalls++
	engine.emit(request.Stage, "正在调用模型")
	result, err := engine.generator.Generate(ctx, request)
	if err != nil {
		return llm.Result{}, fmt.Errorf("%s: %w", request.Stage, err)
	}
	if record {
		if err := engine.store.AppendUsage(request.Stage, result); err != nil {
			return llm.Result{}, err
		}
	}
	return result, nil
}

func loadPromptAndSchema(templateName, schemaName string) (string, map[string]any, error) {
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", nil, fmt.Errorf("读取 Prompt %s 失败: %w", templateName, err)
	}
	data, err := prompts.Schema(schemaName)
	if err != nil {
		return "", nil, fmt.Errorf("读取 Schema %s 失败: %w", schemaName, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return "", nil, fmt.Errorf("解析 Schema %s 失败: %w", schemaName, err)
	}
	return instructions, schema, nil
}

func decodeStructured[T any](text string) (T, error) {
	var target T
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &target); err != nil {
		return target, fmt.Errorf("模型结构化输出不是有效 JSON: %w", err)
	}
	return target, nil
}

func asPrettyJSON(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func generateJSON[T any](
	ctx context.Context,
	engine *Engine,
	stage, templateName, schemaName, input string,
	maxTokens int,
	reasoning string,
) (T, error) {
	var zero T
	instructions, schema, err := loadPromptAndSchema(templateName, schemaName)
	if err != nil {
		return zero, err
	}
	request := llm.Request{
		Stage: stage, Instructions: instructions, Input: input,
		SchemaName: schemaName, Schema: schema,
		MaxOutputTokens: maxTokens, ReasoningEffort: reasoning,
	}
	result, err := engine.generate(ctx, request, true)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr := decodeStructured[T](result.Text)
	if decodeErr == nil {
		return decoded, nil
	}
	if err := engine.saveMalformedOutput(stage, result.Text); err != nil {
		return zero, err
	}
	request.Stage = stage + "_format_repair"
	// 格式修复只携带坏掉的答案，不再重复原始上下文。Schema 已经随请求发送，
	// 因此模型只需补齐括号、逗号或转义；短输入也能显著减少 Flash 模型被截断的概率。
	request.Instructions = "你是 JSON 语法修复器。保持原答案的业务含义，只修复 JSON 语法并补齐 Schema 要求的结构。只返回 JSON。"
	request.Input = "修复下面这份不合法的 JSON：\n\n" + result.Text
	request.ReasoningEffort = "none"
	repaired, err := engine.generate(ctx, request, true)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr = decodeStructured[T](repaired.Text)
	if decodeErr != nil {
		_ = engine.saveMalformedOutput(request.Stage, repaired.Text)
		return zero, decodeErr
	}
	return decoded, nil
}

func (engine *Engine) saveMalformedOutput(stage, output string) error {
	engine.emit(stage, "结构化输出无效，保存原文并重试一次")
	return engine.store.SaveWorking(0, stage+"-malformed.txt", output)
}

func generateText(
	ctx context.Context,
	engine *Engine,
	stage, templateName, input string,
	maxTokens int,
	temperature float64,
) (string, error) {
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", err
	}

	result, err := engine.generate(ctx, llm.Request{
		Stage: stage, Instructions: instructions, Input: input,
		MaxOutputTokens: maxTokens, ReasoningEffort: "none", Temperature: &temperature,
	}, true)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Text) + "\n", nil
}
