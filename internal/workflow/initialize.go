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

// Initialize asks the architect once, validates its durable contracts, then
// atomically creates checkpoint zero. There is no recurring director role.
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	if err := story.ValidateProject(project); err != nil {
		return story.Genesis{}, err
	}
	genesis, usages, err := engine.generateGenesis(ctx, project)
	if err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.Create(project, genesis); err != nil {
		return story.Genesis{}, err
	}
	for _, usage := range usages {
		if err := engine.store.AppendUsage(usage.stage, usage.result); err != nil {
			return story.Genesis{}, err
		}
	}
	engine.emit("architect", "目标读者、叙事承诺、大纲与第 0 章状态已建立")
	return genesis, nil
}

func (engine *Engine) generateGenesis(ctx context.Context, project story.Project) (story.Genesis, []initializationUsage, error) {
	contract := newArchitectInput(project)
	input, err := asPrettyJSON(contract)
	if err != nil {
		return story.Genesis{}, nil, err
	}
	instructions, schema, err := loadPromptAndSchema("architect", "genesis")
	if err != nil {
		return story.Genesis{}, nil, err
	}
	result, err := engine.generate(ctx, architectRequest("architect", instructions, input, schema), false)
	if err != nil {
		return story.Genesis{}, nil, err
	}
	genesis, validationErr := validateArchitectOutput(result.Text)
	usages := []initializationUsage{{stage: "architect", result: result}}
	if validationErr == nil {
		return genesis, usages, nil
	}
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
	return llm.Request{Stage: stage, Instructions: instructions, Input: input,
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

// validateArchitectOutput checks durable structure and references only. It does
// not infer genre semantics from keywords in the user's prose; that judgment
// belongs to the one-time Architect and can later be evaluated by Reader.
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
