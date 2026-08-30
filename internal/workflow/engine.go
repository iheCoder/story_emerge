// Package workflow 实现一个显式、可恢复的小说生产状态机。
// 每个阶段都是普通 Go 方法；所谓“角色”只是职责明确的 Prompt，而不是隐式 Agent Runtime。
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

// Engine 串联“上下文 -> Writer -> Editor -> Story Update -> Reader -> 提交”的状态机。
// usedCalls 从 usage.jsonl 恢复，maxCalls 在整个项目生命周期内生效而非仅限当前进程。
type Engine struct {
	generator llm.Generator
	store     *store.Store
	reporter  Reporter
	options   Options
	maxCalls  int
	usedCalls int
}

// Options 只承载显式、可审计的工作流变体。
// 默认生产入口不传任何选项；实验代码可以注入 Writer 倾向，但不能改写 Bible、Outline、
// Editor 权限或章节提交协议，因此实验变量不会悄悄扩散成新的生产规则。
type Options struct {
	WriterGuidance string
}

func New(generator llm.Generator, files *store.Store, maxCalls int, reporter Reporter) (*Engine, error) {
	return NewWithOptions(generator, files, maxCalls, reporter, Options{})
}

// NewWithOptions 创建带显式实验选项的 Engine。
// 该入口主要供独立 eval 使用；空 Options 与 New 的生产行为完全一致。
func NewWithOptions(generator llm.Generator, files *store.Store, maxCalls int, reporter Reporter, options Options) (*Engine, error) {
	// 构造时恢复历史调用计数，使重启不会重置预算；文件统计失败则拒绝启动，
	// 因为在未知成本下继续生成可能超出用户设定。
	// 恢复已有成功调用数，保证重启不重置项目预算。
	usedCalls, err := files.CountUsage()
	if err != nil {
		return nil, fmt.Errorf("统计既有模型调用失败: %w", err)
	}

	// 组装无状态生成器、文件仓库和可选进度播报器。
	return &Engine{
		generator: generator, store: files, reporter: reporter, options: options,
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

	return repairStructuredJSON[T](ctx, engine, request, result.Text, decodeErr)
}

// repairStructuredJSON 用一次短请求修复已经保存的非法 JSON。
// 它不重新携带原始业务上下文，避免格式错误演变成第二次内容创作。
func repairStructuredJSON[T any](ctx context.Context, engine *Engine, request llm.Request, malformed string, decodeErr error) (T, error) {
	var zero T

	// 修复请求只携带坏答案和精确解析错误，不重复原始创作上下文。
	request = jsonRepairRequest(request, malformed, decodeErr)

	// 执行一次格式修复并再次严格解码；失败即终止该阶段。
	repaired, err := engine.generate(ctx, request, true)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr := decodeStructured[T](repaired.Text)
	if decodeErr != nil {
		_ = engine.saveMalformedOutput(request.Stage, repaired.Text)
		return zero, decodeErr
	}
	return decoded, nil
}

func jsonRepairRequest(request llm.Request, malformed string, decodeErr error) llm.Request {
	request.Stage += "_format_repair"
	request.Instructions = "你是 JSON 结构修复器。保持原答案的业务含义，修复 JSON 语法、字段缺失和字段类型，使输出严格符合随请求提供的 Schema。只返回 JSON。"
	request.Input = "解析错误：\n" + decodeErr.Error() + "\n\n修复下面的 JSON：\n\n" + malformed
	request.ReasoningEffort = "none"

	return request
}

// saveMalformedOutput 把无法解码的原文保存到 .work 以便人工诊断。
func (engine *Engine) saveMalformedOutput(stage, output string) error {
	// 结构化失败原文进入 .work 而不是覆盖正式产物，既可诊断又不会污染可恢复状态。
	// 通知当前阶段发生格式失败，便于 CLI 解释为什么出现额外调用。
	engine.emit(stage, "结构化输出无效，保存原文并重试一次")

	// 保存原文作为修复前证据；即使修复再次失败也能人工定位供应商返回。
	return engine.store.SaveWorking(chapterNumberFromStage(stage), stage+"-malformed.txt", output)
}

// chapterNumberFromStage 从统一阶段名恢复故障所属章节。
// 初始化或无法识别的阶段安全归入 000，不会被误当成正式章节产物。
func chapterNumberFromStage(stage string) int {
	parts := strings.Split(stage, "_")
	if len(parts) < 2 || parts[0] != "chapter" {
		return 0
	}

	number, err := strconv.Atoi(parts[1])
	if err != nil || number < 1 {
		return 0
	}

	return number
}

// generateText 执行一个纯文本模型阶段，并统一去除首尾空白后补齐换行。
func generateText(
	ctx context.Context,
	engine *Engine,
	stage, templateName, input string,
	maxTokens int,
	temperature float64,
) (string, error) {
	return generateTextWithGuidance(ctx, engine, stage, templateName, input, "", maxTokens, temperature)
}

// generateTextWithGuidance 只为显式实验变体追加系统级创作指导。
// 空 guidance 直接保留嵌入 Prompt 原文，确保正常生产调用不存在隐藏差异。
func generateTextWithGuidance(
	ctx context.Context,
	engine *Engine,
	stage, templateName, input, guidance string,
	maxTokens int,
	temperature float64,
) (string, error) {
	// 纯文本阶段不需要 Schema，但仍复用 generate 的预算、超时、重试和用量记录机制。
	// 读取纯文本角色 Prompt。
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", err
	}
	instructions = appendExperimentalGuidance(instructions, guidance)

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

func appendExperimentalGuidance(instructions, guidance string) string {
	guidance = strings.TrimSpace(guidance)
	if guidance == "" {
		return instructions
	}

	return strings.TrimSpace(instructions) + "\n\n# 本次独立实验指导\n\n" + guidance
}
