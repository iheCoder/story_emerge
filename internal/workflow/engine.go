// Package workflow 实现一个显式、可恢复的小说生产状态机。
// 每个阶段都是普通 Go 方法；所谓“角色”只是职责明确的 Prompt，而不是隐式 Agent Runtime。
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

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

// Engine 串联 Planner -> Writer -> Editor -> Commit 的章节循环。
// usedCalls 从 usage.jsonl 恢复成功调用数；本进程中的失败调用也占额度，但不写入该文件。
type Engine struct {
	generator llm.Generator
	store     *store.Store
	reporter  Reporter
	maxCalls  int
	usedCalls int
}

// New 组装工作流并恢复已记录的成功调用数，避免重启把这部分项目预算清零。
func New(generator llm.Generator, files *store.Store, maxCalls int, reporter Reporter) (*Engine, error) {
	// 预算必须为正；历史用量统计失败就拒绝启动，不能在不知道已用额度时继续生成。
	if maxCalls < 1 {
		return nil, fmt.Errorf("模型调用预算必须大于 0")
	}
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
func (engine *Engine) emit(stage, message string, attrs ...slog.Attr) {
	engine.log(slog.LevelInfo, stage, message, attrs...)

	// 进度通知是旁路能力，不能因 reporter 缺失阻断核心写作流程。
	if engine.reporter != nil {
		engine.reporter(Event{Stage: stage, Message: message})
	}
}

// generate 统一执行预算检查、调用和用量落盘，避免某个新增阶段绕过成本控制。
func (engine *Engine) generate(ctx context.Context, request llm.Request) (llm.Result, error) {
	// 先解析角色参数，再记录日志和占用调用预算。配置错误不会被误记为一次模型调用。
	// 生产 RoleClient 提供 YAML 参数解析；没有该能力的测试生成器使用同一组内置默认值。
	// 保持 Generator 接口只负责生成，不要求每个替身都实现配置解析。
	if resolver, ok := engine.generator.(interface {
		ResolveRequest(llm.Request) (llm.Request, error)
	}); ok {
		var err error
		request, err = resolver.ResolveRequest(request)
		if err != nil {
			return llm.Result{}, err
		}
	} else {
		request = llm.WithRoleDefaults(request)
	}

	// 先检查取消和项目调用额度，再占用一个逻辑调用槽位。
	// 当前进程中失败尝试也计数；只有成功结果写入 usage.jsonl，重启不会恢复失败计数。
	if err := ctx.Err(); err != nil {
		return llm.Result{}, err
	}
	if engine.usedCalls >= engine.maxCalls {
		return llm.Result{}, fmt.Errorf("已达到模型调用上限 %d", engine.maxCalls)
	}
	engine.usedCalls++

	// 日志记录解析后的实际参数：max_calls 限制调用次数，max_output_tokens 限制单次输出。
	// 两者是不同的预算，便于区分“项目额度耗尽”和“单次模型响应被截断”。
	engine.emit(request.Stage, "正在调用模型",
		slog.String("role", request.Role), slog.Int("call", engine.usedCalls),
		slog.Int("max_calls", engine.maxCalls), slog.Int("max_output_tokens", request.MaxOutputTokens),
		slog.String("reasoning_effort", request.ReasoningEffort))

	// 调用具体模型实现；错误带上阶段名后返回。
	started := time.Now()
	result, err := engine.generator.Generate(ctx, request)
	attrs := []slog.Attr{
		slog.String("role", request.Role), slog.Int64("duration_ms", time.Since(started).Milliseconds()),
	}
	if result.Model != "" {
		attrs = append(attrs, slog.String("model", result.Model))
	}
	if result.ResponseID != "" {
		attrs = append(attrs, slog.String("response_id", result.ResponseID))
	}
	// 网络失败可能没有供应商用量，缺失信息不能被日志伪装成“消耗为零”。
	if result.Usage != (llm.Usage{}) {
		attrs = append(attrs, slog.Int("input_tokens", result.Usage.InputTokens), slog.Int("output_tokens", result.Usage.OutputTokens))
	}
	if err != nil {
		engine.log(slog.LevelError, request.Stage, "模型请求失败", append(attrs, slog.String("error", err.Error()))...)
		// 不完整响应不能进入 Commit，但其已返回文本仍是定位截断位置的有效证据。
		// 仅放入诊断目录，不尝试把半截 JSON 补成可提交事实。
		if result.Text != "" {
			if saveErr := engine.store.SaveWorking(chapterNumberFromStage(request.Stage), request.Stage+"-incomplete.txt", result.Text); saveErr != nil {
				engine.log(slog.LevelError, request.Stage, "保存不完整响应失败", slog.String("error", saveErr.Error()))
			}
		}
		return llm.Result{}, fmt.Errorf("%s: %w", request.Stage, err)
	}
	engine.log(slog.LevelInfo, request.Stage, "模型请求成功", attrs...)

	// 初始化和逐章阶段都在已准备的项目目录中运行，成功用量统一立即落盘。
	if err := engine.store.AppendUsage(request.Stage, result); err != nil {
		return llm.Result{}, err
	}
	return result, nil
}

// loadPromptAndSchema 同时加载角色指令和结构化输出 Schema。
func loadPromptAndSchema(templateName, schemaName string) (string, map[string]any, error) {
	// Prompt 与 Schema 都从 embed 资源读取；每次构造角色请求时检查资源和 JSON 格式。
	// 模板缺失时直接返回本地错误，不发起模型调用。
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
	// 不尝试猜测或补写业务字段；仅语法与类型问题允许一次独立的格式修复。
	var target T
	cleaned := cleanJSON(text)

	// 严格解码为目标类型，不对业务字段进行猜测修补。
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(cleaned)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return target, fmt.Errorf("模型结构化输出不是有效 JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return target, fmt.Errorf("结构化输出包含多余内容")
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
	stage, role, templateName, schemaName, input string,
) (T, error) {
	// 结构化阶段最多经历“正常生成 -> JSON 解码 -> 一次格式修复”三步。
	// 修复仍受同一 maxCalls 预算约束，第二次失败直接终止，不无限循环消耗额度。
	var zero T
	instructions, schema, err := loadPromptAndSchema(templateName, schemaName)
	if err != nil {
		return zero, err
	}

	// 普通业务请求只声明角色和输出结构，思考强度、输出上限由统一入口解析角色配置。
	// 这样调大 Commit 上限只需改 YAML，不必逐一修改业务阶段和格式修复代码。
	request := llm.Request{
		Stage: stage, Role: role, Instructions: instructions, Input: input,
		SchemaName: schemaName, Schema: schema,
	}

	// 执行正常结构化生成并尝试解码。
	result, err := engine.generate(ctx, request)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr := decodeStructured[T](result.Text)
	if decodeErr == nil {
		// 语法已合法但必要字段缺失时直接失败，不能让格式修复替提取器创造事实。
		if err := validateOutputShape(result.Text, schema); err != nil {
			if saveErr := engine.saveMalformedOutput(stage, result.Text); saveErr != nil {
				return zero, saveErr
			}
			return zero, fmt.Errorf("%s 输出结构无效: %w", stage, err)
		}
		return decoded, nil
	}

	// 先保存坏原文再修复，保证即使修复请求也失败，人工仍能看到模型真实返回。
	if err := engine.saveMalformedOutput(stage, result.Text); err != nil {
		return zero, err
	}

	// 完整的自然语言评论不是待修复的 JSON，不能借格式修复重新编造业务结果。
	cleaned := cleanJSON(result.Text)
	if !strings.HasPrefix(cleaned, "{") {
		return zero, decodeErr
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
	repaired, err := engine.generate(ctx, request)
	if err != nil {
		return zero, err
	}
	decoded, decodeErr := decodeStructured[T](repaired.Text)
	if decodeErr != nil {
		_ = engine.saveMalformedOutput(request.Stage, repaired.Text)
		return zero, decodeErr
	}
	if err := validateOutputShape(repaired.Text, request.Schema); err != nil {
		if saveErr := engine.saveMalformedOutput(request.Stage, repaired.Text); saveErr != nil {
			return zero, saveErr
		}
		return zero, err
	}
	return decoded, nil
}

// jsonRepairRequest 保留原角色和 Schema，只把任务收窄为修复已有答案的格式。
func jsonRepairRequest(request llm.Request, malformed string, decodeErr error) llm.Request {
	request.Stage += "_format_repair"
	request.Instructions = "你是 JSON 结构修复器。保持原答案的业务含义，修复 JSON 语法和字段类型；不得补写原答案没有的业务信息，使输出严格符合随请求提供的 Schema。只返回 JSON。"
	request.Input = "解析错误：\n" + decodeErr.Error() + "\n\n修复下面的 JSON：\n\n" + malformed

	// 格式修复不需要重新推演故事；显式 none 优先于角色配置，输出上限仍沿用原请求规则。
	request.ReasoningEffort = "none"

	return request
}

// saveMalformedOutput 把无法解码的原文保存到 .work 以便人工诊断。
func (engine *Engine) saveMalformedOutput(stage, output string) error {
	// 结构化失败原文进入 .work 而不是覆盖正式产物，既可诊断又不会污染可恢复状态。
	// 通知当前阶段发生格式失败，便于 CLI 解释为什么出现额外调用。
	engine.emit(stage, "结构化输出无效，保存原文以便诊断")

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
	stage, role, templateName, input string,
	temperature float64,
) (string, error) {
	// 纯文本阶段不需要 Schema；只加载对应正文任务的 Prompt，调用仍走统一入口。
	instructions, err := prompts.Template(templateName)
	if err != nil {
		return "", err
	}

	// 温度由初稿或修订任务指定；思考强度与输出上限统一读取 Writer 角色配置。
	// 预算、用量记录由 Engine 处理，生产客户端负责网络超时和有限重试。
	result, err := engine.generate(ctx, llm.Request{
		Stage: stage, Role: role, Instructions: instructions, Input: input,
		Temperature: &temperature,
	})
	if err != nil {
		return "", err
	}

	// 规范正文首尾空白，保证保存文件拥有稳定换行。
	return strings.TrimSpace(result.Text) + "\n", nil
}
