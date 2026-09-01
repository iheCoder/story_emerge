package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

const architectMaxOutputTokens = 16000

type architectInput struct {
	Idea          string `json:"idea"`
	LengthProfile string `json:"length_profile"`
	CreativeRule  string `json:"creative_rule"`
}

type genesisRepairInput struct {
	Contract        architectInput `json:"original_contract"`
	RejectedOutput  string         `json:"rejected_output"`
	ValidationError string         `json:"validation_error"`
}

type initializationUsage struct {
	stage  string
	result llm.Result
}

// Initialize 只调用一次 Architect，验证稳定契约后原子创建第 0 章。
// Reader 必须读完真实章节后才产生 Observation，初始化阶段不预填主观意见。
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	// 阶段一：在模型调用前验证项目输入，避免为无效任务消耗预算。
	if err := story.ValidateProject(project); err != nil {
		return story.Genesis{}, err
	}

	// 阶段二：让 Architect 建立一次性的 Bible、Outline 和精简初态。
	genesis, usages, err := engine.generateGenesis(ctx, project)
	if err != nil {
		return story.Genesis{}, err
	}

	// 阶段三：Store 完整写入第 0 章后才创建 HEAD。
	if err := engine.store.Create(project, genesis); err != nil {
		return story.Genesis{}, err
	}

	// 阶段四：初始化成功后补记调用用量，避免半初始化项目伪装成可运行状态。
	for _, usage := range usages {
		if err := engine.store.AppendUsage(usage.stage, usage.result); err != nil {
			return story.Genesis{}, err
		}
	}

	engine.emit("architect", "目标读者、Story Spine、Outline 与精简初态已建立")
	return genesis, nil
}

func (engine *Engine) generateGenesis(ctx context.Context, project story.Project) (story.Genesis, []initializationUsage, error) {
	// 阶段一：把用户输入编码成 Architect 唯一可见的创作合同。
	contract := newArchitectInput(project)
	input, err := asPrettyJSON(contract)
	if err != nil {
		return story.Genesis{}, nil, err
	}
	instructions, schema, err := loadPromptAndSchema("architect", "genesis")
	if err != nil {
		return story.Genesis{}, nil, err
	}

	// 阶段二：执行首次生成，并同时保留成功调用的用量记录。
	result, err := engine.generate(ctx, architectRequest("architect", instructions, input, schema), false)
	if err != nil {
		return story.Genesis{}, nil, err
	}
	genesis, validationErr := validateArchitectOutput(result.Text)
	usages := []initializationUsage{{stage: "architect", result: result}}
	if validationErr == nil {
		return genesis, usages, nil
	}

	// 阶段三：结构或引用无效时只修复一次，不重新设计整个故事。
	repaired, err := engine.repairGenesis(ctx, contract, result.Text, validationErr, schema)
	if err != nil {
		return story.Genesis{}, nil, err
	}
	genesis, err = validateArchitectOutput(repaired.Text)
	if err != nil {
		return story.Genesis{}, nil, fmt.Errorf("架构师修复后的故事起点仍无效: %w", err)
	}
	return genesis, append(usages, initializationUsage{stage: "architect_repair", result: repaired}), nil
}

func architectRequest(stage, instructions, input string, schema map[string]any) llm.Request {
	return llm.Request{Stage: stage, Role: llm.RoleArchitect, Instructions: instructions, Input: input,
		SchemaName: "novel_genesis", Schema: schema, MaxOutputTokens: architectMaxOutputTokens, ReasoningEffort: "low"}
}

func (engine *Engine) repairGenesis(ctx context.Context, contract architectInput, rejected string, validationErr error, schema map[string]any) (llm.Result, error) {
	input, err := asPrettyJSON(genesisRepairInput{Contract: contract, RejectedOutput: rejected, ValidationError: validationErr.Error()})
	if err != nil {
		return llm.Result{}, err
	}
	instructions := "保持用户给定的人物身份、关系、类型与故事方向，只修复 JSON、缺失字段和引用错误。不得用大众人口标签替代具体目标读者。只返回 JSON。"
	return engine.generate(ctx, architectRequest("architect_repair", instructions, input, schema), false)
}

// validateArchitectOutput 只检查稳定结构和引用，不从用户文本关键词推断题材语义。
func validateArchitectOutput(output string) (story.Genesis, error) {
	genesis, err := decodeStructured[story.Genesis](output)
	if err != nil {
		return story.Genesis{}, err
	}
	if err := story.ValidateGenesis(genesis); err != nil {
		return story.Genesis{}, fmt.Errorf("架构师交付的故事起点无效: %w", err)
	}
	return genesis, nil
}

func newArchitectInput(project story.Project) architectInput {
	return architectInput{
		Idea: project.Idea, LengthProfile: project.LengthProfile,
		CreativeRule: "篇幅仅表示故事规模，不得转换为固定章数、每章字数、scene 数或转折配额；完整保留用户明确给出的人物身份、关系和类型；目标读者必须像一个有姓名、经历和明确偏好的真人",
	}
}
