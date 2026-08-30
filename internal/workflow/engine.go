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

// Event 是 CLI/测试可观察的阶段进度，不携带模型原文或敏感配置。
type Event struct {
	Stage   string
	Message string
}

// Reporter 允许入口层订阅进度；Engine 在无 reporter 时仍可无界面运行。
type Reporter func(Event)

// Engine 串联“上下文 -> 计划 -> 正文 -> 审核 -> 提交”的状态机。
// usedCalls 从 usage.jsonl 恢复，maxCalls 在整个项目生命周期内生效而非仅限当前进程。
type Engine struct {
	generator llm.Generator
	store     *store.Store
	reporter  Reporter
	maxCalls  int
	usedCalls int
}

func New(generator llm.Generator, files *store.Store, maxCalls int, reporter Reporter) (*Engine, error) {
	// 构造时恢复历史调用计数，使重启不会重置预算；文件统计失败则拒绝启动，
	// 因为在未知成本下继续生成可能超出用户设定。
	// 恢复已有成功调用数，保证重启不重置项目预算。
	usedCalls, err := files.CountUsage()
	if err != nil {
		return nil, fmt.Errorf("统计既有模型调用失败: %w", err)
	}

	// 组装无状态生成器、文件仓库和可选进度播报器。
	return &Engine{
		generator: generator, store: files, reporter: reporter,
		maxCalls: maxCalls, usedCalls: usedCalls,
	}, nil
}

// emit 向可选观察者发送一条阶段进度事件。
func (engine *Engine) emit(stage, message string) {
	// 进度通知是旁路能力，不能因 reporter 缺失阻断核心写作流程。
	if engine.reporter != nil {
		engine.reporter(Event{Stage: stage, Message: message})
	}
}

// generate 统一执行预算检查、调用和用量落盘，避免某个新增阶段绕过成本控制。
func (engine *Engine) generate(ctx context.Context, request llm.Request, record bool) (llm.Result, error) {
	// 预算检查发生在调用前；计数在发起请求前递增，保守地把失败尝试也视为已占用的逻辑槽位，
	// 防止瞬时故障下的重试/重启绕过成本护栏。成功结果才由 store 记入 usage.jsonl。
	// 在发起请求前检查并占用一个逻辑调用槽位。
	if engine.maxCalls > 0 && engine.usedCalls >= engine.maxCalls {
		return llm.Result{}, fmt.Errorf("已达到模型调用上限 %d", engine.maxCalls)
	}
	engine.usedCalls++
	engine.emit(request.Stage, "正在调用模型")

	// 调用具体模型实现；错误带上阶段名后返回。
	result, err := engine.generator.Generate(ctx, request)
	if err != nil {
		return llm.Result{}, fmt.Errorf("%s: %w", request.Stage, err)
	}

	// 按调用方要求记录成功结果的用量。
	if record {
		if err := engine.store.AppendUsage(request.Stage, result); err != nil {
			return llm.Result{}, err
		}
	}
	return result, nil
}

// loadPromptAndSchema 同时加载角色指令和结构化输出 Schema。
func loadPromptAndSchema(templateName, schemaName string) (string, map[string]any, error) {
	// Prompt 与 Schema 都从 embed 资源读取，启动时校验其存在和 JSON 合法性，
	// 避免运行到中途才发现部署缺少模板。
	// 读取角色 Prompt。
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", nil, fmt.Errorf("读取 Prompt %s 失败: %w", templateName, err)
	}

	// 读取并解析结构化输出 Schema。
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

// decodeStructured 清理可选代码围栏并把模型文本解码为目标领域类型。
func decodeStructured[T any](text string) (T, error) {
	// 模型偶尔会包裹 Markdown 代码围栏；这里只做可解释的外层清理，
	// 不尝试猜测或修补业务字段，字段修复交给一次独立的 format_repair 调用。
	// 去除模型可能添加的 Markdown 代码围栏。
	var target T
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")

	// 严格解码为目标类型，不对业务字段进行猜测修补。
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &target); err != nil {
		return target, fmt.Errorf("模型结构化输出不是有效 JSON: %w", err)
	}
	return target, nil
}

// asPrettyJSON 将阶段输入或中间对象编码为适合模型阅读的缩进 JSON。
func asPrettyJSON(value any) (string, error) {
	// 统一使用缩进 JSON 作为阶段间输入，便于模型阅读，也便于人工从 .work 复盘。
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// generateJSON 执行带 Schema 的模型阶段，并在解码失败时进行一次格式修复。
func generateJSON[T any](
	ctx context.Context,
	engine *Engine,
	stage, templateName, schemaName, input string,
	maxTokens int,
	reasoning string,
) (T, error) {
	// 结构化阶段最多经历“正常生成 -> JSON 解码 -> 一次格式修复”三步。
	// 修复仍受同一 maxCalls 预算约束，第二次失败直接终止，不无限循环消耗额度。
	// 加载 Prompt/Schema，并构造结构化请求。
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

	// 执行正常结构化生成并尝试解码。
	result, err := engine.generate(ctx, request, true)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr := decodeStructured[T](result.Text)
	if decodeErr == nil {
		return decoded, nil
	}

	// 先保存坏原文再修复，保证即使修复请求也失败，人工仍能看到模型真实返回。
	// 保存坏原文，保留人工诊断证据。
	if err := engine.saveMalformedOutput(stage, result.Text); err != nil {
		return zero, err
	}

	// 缩短上下文，只让修复模型处理坏 JSON；仍受同一调用预算约束。
	request.Stage = stage + "_format_repair"
	// 格式修复只携带坏掉的答案，不再重复原始上下文。Schema 已经随请求发送，
	// 因此模型只需补齐括号、逗号或转义；短输入也能显著减少 Flash 模型被截断的概率。
	request.Instructions = "你是 JSON 语法修复器。保持原答案的业务含义，只修复 JSON 语法并补齐 Schema 要求的结构。只返回 JSON。"
	request.Input = "修复下面这份不合法的 JSON：\n\n" + result.Text
	request.ReasoningEffort = "none"

	// 执行一次格式修复并再次严格解码；失败即终止该阶段。
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

// saveMalformedOutput 把无法解码的原文保存到 .work 以便人工诊断。
func (engine *Engine) saveMalformedOutput(stage, output string) error {
	// 结构化失败原文进入 .work 而不是覆盖正式产物，既可诊断又不会污染可恢复状态。
	// 通知当前阶段发生格式失败，便于 CLI 解释为什么出现额外调用。
	engine.emit(stage, "结构化输出无效，保存原文并重试一次")

	// 保存原文作为修复前证据；即使修复再次失败也能人工定位供应商返回。
	return engine.store.SaveWorking(0, stage+"-malformed.txt", output)
}

// generateText 执行一个纯文本模型阶段，并统一去除首尾空白后补齐换行。
func generateText(
	ctx context.Context,
	engine *Engine,
	stage, templateName, input string,
	maxTokens int,
	temperature float64,
) (string, error) {
	// 纯文本阶段不需要 Schema，但仍复用 generate 的预算、超时、重试和用量记录机制。
	// 读取纯文本角色 Prompt。
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", err
	}

	// 复用统一预算/重试/用量链路生成正文。
	result, err := engine.generate(ctx, llm.Request{
		Stage: stage, Instructions: instructions, Input: input,
		MaxOutputTokens: maxTokens, ReasoningEffort: "none", Temperature: &temperature,
	}, true)
	if err != nil {
		return "", err
	}

	// 规范正文首尾空白，保证保存文件拥有稳定换行。
	return strings.TrimSpace(result.Text) + "\n", nil
}
