package workflow

import (
	"context"
	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

type architectInput struct {
	UserIdea     string `json:"user_idea"`
	TargetLength string `json:"target_length"`
}

// Initialize 先原样保存用户输入，再建立一次性的作品核心与第 0 章状态。
// Architect 此后不再被调用，JSON 格式修复仍沿用 architect 的模型配置。
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (story.Genesis, error) {
	if err := engine.store.Prepare(project); err != nil {
		return story.Genesis{}, err
	}
	input, err := asPrettyJSON(architectInput{UserIdea: project.Idea, TargetLength: story.LengthGoal(project.LengthProfile)})
	if err != nil {
		return story.Genesis{}, err
	}
	genesis, err := generateJSON[story.Genesis](ctx, engine, "architect", llm.RoleArchitect, "architect", "genesis", input, 16000, "low")
	if err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.SaveWorking(0, "genesis.json", genesis); err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.CommitGenesis(genesis); err != nil {
		return story.Genesis{}, err
	}
	engine.emit("architect", "作品核心、初始事实与当前方向已建立")
	return genesis, nil
}
