package workflow

import (
	"context"
	"fmt"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

type architectInput struct {
	Idea            string `json:"idea"`
	TargetChapters  int    `json:"target_chapters"`
	ChapterMinChars int    `json:"chapter_min_chars"`
	ChapterMaxChars int    `json:"chapter_max_chars"`
	ProductBoundary string `json:"product_boundary"`
}

// Initialize 只调用一次总导演，成功后建立第 0 章检查点。
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	if err := story.ValidateProject(project); err != nil {
		return story.Genesis{}, err
	}
	input, err := asPrettyJSON(newArchitectInput(project))
	if err != nil {
		return story.Genesis{}, err
	}
	instructions, schema, err := loadPromptAndSchema("architect", "genesis")
	if err != nil {
		return story.Genesis{}, err
	}
	result, err := engine.generate(ctx, llm.Request{
		Stage: "architect", Instructions: instructions, Input: input,
		SchemaName: "novel_genesis", Schema: schema,
		MaxOutputTokens: 12000, ReasoningEffort: "low",
	}, false)
	if err != nil {
		return story.Genesis{}, err
	}
	genesis, err := decodeStructured[story.Genesis](result.Text)
	if err != nil {
		return story.Genesis{}, err
	}
	if err := story.ValidateGenesis(genesis, project.TargetChapters); err != nil {
		return story.Genesis{}, fmt.Errorf("总导演交付的故事起点无效: %w", err)
	}
	if err := engine.store.Create(project, genesis); err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.AppendUsage("architect", result); err != nil {
		return story.Genesis{}, err
	}
	engine.emit("architect", "故事圣经与第 0 章状态已建立")
	return genesis, nil
}

func newArchitectInput(project story.Project) architectInput {
	return architectInput{
		Idea: project.Idea, TargetChapters: project.TargetChapters,
		ChapterMinChars: project.ChapterMinChars, ChapterMaxChars: project.ChapterMaxChars,
		ProductBoundary: "中文类型中篇；核心人物不超过 8；单主线为主；结局完整；网络爽文必须有行动、兑现和代价",
	}
}
