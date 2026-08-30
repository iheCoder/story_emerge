package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

const architectMaxOutputTokens = 16000

// architectInput 是总导演唯一需要的项目级输入，刻意不携带 API 密钥等运行机密。
type architectInput struct {
	Idea                string `json:"idea"`
	LengthProfile       string `json:"length_profile,omitempty"`
	TargetChapters      int    `json:"target_chapters,omitempty"`
	AllowedChapterRange string `json:"allowed_chapter_range"`
	ChapterMinChars     int    `json:"chapter_min_chars"`
	ChapterMaxChars     int    `json:"chapter_max_chars"`
	ProductBoundary     string `json:"product_boundary"`
}

// genesisRepairInput 把原始创作边界、被拒输出和确定性错误交回总导演。
// 修复阶段只允许补齐结构或修正越界字段，不能改变用户的故事点子。
type genesisRepairInput struct {
	Contract        architectInput `json:"original_contract"`
	RejectedOutput  string         `json:"rejected_output"`
	ValidationError string         `json:"validation_error"`
}

type initializationUsage struct {
	stage  string
	result llm.Result
}

// Initialize 调用总导演生成 Genesis，并在验证后创建第 0 章项目快照。
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	// 初始化是一条不可拆分的“生成 -> 一次有界修复 -> 提交”流程。
	// Create 成功前没有 HEAD，任何失败都不会伪装成可继续项目。
	if err := story.ValidateProject(project); err != nil {
		return story.Genesis{}, err
	}

	genesis, targetChapters, usages, err := engine.generateGenesis(ctx, project)
	if err != nil {
		return story.Genesis{}, err
	}
	project.TargetChapters = targetChapters
	genesis.TargetChapters = targetChapters

	if err := engine.store.Create(project, genesis); err != nil {
		return story.Genesis{}, err
	}
	for _, usage := range usages {
		if err := engine.store.AppendUsage(usage.stage, usage.result); err != nil {
			return story.Genesis{}, err
		}
	}

	engine.emit("architect", "故事圣经与第 0 章状态已建立")
	return genesis, nil
}

// generateGenesis 执行总导演阶段，并对 JSON 或领域校验失败只修复一次。
func (engine *Engine) generateGenesis(
	ctx context.Context,
	project story.Project,
) (story.Genesis, int, []initializationUsage, error) {
	contract := newArchitectInput(project)
	input, err := asPrettyJSON(contract)
	if err != nil {
		return story.Genesis{}, 0, nil, err
	}
	instructions, schema, err := loadPromptAndSchema("architect", "genesis")
	if err != nil {
		return story.Genesis{}, 0, nil, err
	}

	result, err := engine.generate(ctx, architectRequest("architect", instructions, input, schema), false)
	if err != nil {
		return story.Genesis{}, 0, nil, err
	}
	genesis, target, validationErr := validateArchitectOutput(project, result.Text)
	usages := []initializationUsage{{stage: "architect", result: result}}
	if validationErr == nil {
		return genesis, target, usages, nil
	}

	repaired, err := engine.repairGenesis(ctx, contract, result.Text, validationErr, schema)
	if err != nil {
		return story.Genesis{}, 0, nil, err
	}
	genesis, target, err = validateArchitectOutput(project, repaired.Text)
	if err != nil {
		return story.Genesis{}, 0, nil, fmt.Errorf("总导演修复后的故事起点仍无效: %w", err)
	}

	return genesis, target, append(usages, initializationUsage{stage: "architect_repair", result: repaired}), nil
}

func architectRequest(stage, instructions, input string, schema map[string]any) llm.Request {
	return llm.Request{
		Stage: stage, Instructions: instructions, Input: input,
		SchemaName: "novel_genesis", Schema: schema,
		MaxOutputTokens: architectMaxOutputTokens, ReasoningEffort: "low",
	}
}

// repairGenesis 用确定性错误约束一次结构化修复，避免无界重试和故事漂移。
func (engine *Engine) repairGenesis(
	ctx context.Context,
	contract architectInput,
	rejected string,
	validationErr error,
	schema map[string]any,
) (llm.Result, error) {
	input, err := asPrettyJSON(genesisRepairInput{
		Contract: contract, RejectedOutput: rejected, ValidationError: validationErr.Error(),
	})
	if err != nil {
		return llm.Result{}, err
	}
	instructions := "你是小说 Genesis 修复器。保持用户点子、人物身份、关系与故事方向不变，只修复 JSON 语法、缺失字段、引用、章节范围和验证错误。未明确的身份信息不得擅自推断。只返回符合 Schema 的 JSON。"
	return engine.generate(ctx, architectRequest("architect_repair", instructions, input, schema), false)
}

// validateArchitectOutput 同时检查 JSON、章节选择和完整领域契约。
func validateArchitectOutput(project story.Project, output string) (story.Genesis, int, error) {
	genesis, err := decodeStructured[story.Genesis](output)
	if err != nil {
		return story.Genesis{}, 0, err
	}
	target, err := resolveTargetChapters(project, genesis.TargetChapters)
	if err != nil {
		return story.Genesis{}, 0, err
	}
	genesis.TargetChapters = target
	if err := story.ValidateGenesis(genesis, target); err != nil {
		return story.Genesis{}, 0, fmt.Errorf("总导演交付的故事起点无效: %w", err)
	}
	return genesis, target, nil
}

// newArchitectInput 提取总导演需要的项目参数并附加 V1 产品边界。
func newArchitectInput(project story.Project) architectInput {
	// ProductBoundary 是 V1 的产品护栏：小人物表、单主线、完整结局和“行动-兑现-代价”
	// 让网络爽文具备事件因果，而不是只生成情绪口号。
	return architectInput{
		Idea: project.Idea, LengthProfile: project.LengthProfile,
		TargetChapters: project.TargetChapters, AllowedChapterRange: allowedChapterRange(project),
		ChapterMinChars: project.ChapterMinChars, ChapterMaxChars: project.ChapterMaxChars,
		ProductBoundary: "中文类型小说；核心人物不超过 8；单主线为主；结局完整；必须保留用户明确给出的身份与关系；未明确的性别等身份信息不得擅自推断；网络爽文必须有行动、兑现和代价",
	}
}

// allowedChapterRange 告诉总导演可选边界；CLI 的显式章数会收敛为单点范围。
func allowedChapterRange(project story.Project) string {
	if project.TargetChapters > 0 {
		return fmt.Sprintf("%d", project.TargetChapters)
	}
	minimum, maximum, _ := story.ChapterRangeForLength(project.LengthProfile)
	return fmt.Sprintf("%d-%d", minimum, maximum)
}

// resolveTargetChapters 校验总导演的章节选择，并兼容已有显式章节数的 CLI 项目。
func resolveTargetChapters(project story.Project, selected int) (int, error) {
	if project.TargetChapters > 0 {
		if selected != 0 && selected != project.TargetChapters {
			return 0, fmt.Errorf("总导演选择的章节数 %d 与项目指定值 %d 不一致", selected, project.TargetChapters)
		}
		return project.TargetChapters, nil
	}

	minimum, maximum, valid := story.ChapterRangeForLength(project.LengthProfile)
	if !valid || selected < minimum || selected > maximum {
		return 0, fmt.Errorf("总导演选择的章节数 %d 不在篇幅档位范围 %d-%d 内", selected, minimum, maximum)
	}
	return selected, nil
}
