package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

// architectInput 是总导演唯一需要的项目级输入，刻意不携带 API 密钥等运行机密。
type architectInput struct {
	Idea            string `json:"idea"`
	TargetChapters  int    `json:"target_chapters"`
	ChapterMinChars int    `json:"chapter_min_chars"`
	ChapterMaxChars int    `json:"chapter_max_chars"`
	ProductBoundary string `json:"product_boundary"`
}

// Initialize 调用总导演生成 Genesis，并在验证后创建第 0 章项目快照。
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	// 初始化是一条不可拆分的“生成并提交”流程：校验项目 -> 调总导演 -> 校验 Genesis -> 写第 0 章。
	// 在 Create 成功前不会产生 HEAD，因此中途失败的响应只能作为错误返回，不会伪装成可运行项目。
	// 先校验项目契约，拒绝无法稳定执行的章节范围/预算。
	if err := story.ValidateProject(project); err != nil {
		return story.Genesis{}, err
	}

	// 准备总导演输入，只传项目参数和产品边界，不传运行机密。
	input, err := asPrettyJSON(newArchitectInput(project))
	if err != nil {
		return story.Genesis{}, err
	}

	// 加载总导演 Prompt/Schema。
	instructions, schema, err := loadPromptAndSchema("architect", "genesis")
	if err != nil {
		return story.Genesis{}, err
	}

	// 执行唯一一次初始化生成。
	result, err := engine.generate(ctx, llm.Request{
		Stage: "architect", Instructions: instructions, Input: input,
		SchemaName: "novel_genesis", Schema: schema,
		MaxOutputTokens: 12000, ReasoningEffort: "low",
	}, false)
	if err != nil {
		return story.Genesis{}, err
	}

	// 解码并校验模型交付。
	// 合法 JSON 仍可能违反人物、阶段或剧情线约束，所以必须经过领域校验。
	genesis, err := decodeStructured[story.Genesis](result.Text)
	if err != nil {
		return story.Genesis{}, err
	}
	if err := story.ValidateGenesis(genesis, project.TargetChapters); err != nil {
		// 模型返回合法 JSON 不代表故事结构可执行；这里把领域校验放在任何落盘之前。
		return story.Genesis{}, fmt.Errorf("总导演交付的故事起点无效: %w", err)
	}

	// 持久化第 0 章和人读视图。
	// Store.Create 会把 HEAD 放在所有初始化产物之后，保证失败不会生成假项目。
	if err := engine.store.Create(project, genesis); err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.AppendUsage("architect", result); err != nil {
		return story.Genesis{}, err
	}

	// 通知调用方初始化已完成。
	engine.emit("architect", "故事圣经与第 0 章状态已建立")
	return genesis, nil
}

// newArchitectInput 提取总导演需要的项目参数并附加 V1 产品边界。
func newArchitectInput(project story.Project) architectInput {
	// ProductBoundary 是 V1 的产品护栏：小人物表、单主线、完整结局和“行动-兑现-代价”
	// 让网络爽文具备事件因果，而不是只生成情绪口号。
	return architectInput{
		Idea: project.Idea, TargetChapters: project.TargetChapters,
		ChapterMinChars: project.ChapterMinChars, ChapterMaxChars: project.ChapterMaxChars,
		ProductBoundary: "中文类型中篇；核心人物不超过 8；单主线为主；结局完整；网络爽文必须有行动、兑现和代价",
	}
}
