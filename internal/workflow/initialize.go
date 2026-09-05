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
func (engine *Engine) Initialize(ctx context.Context, project story.Project) (genesis story.Genesis, err error) {
	// 先建立项目目录并保存原始创意，保证模型请求失败时用户输入与诊断位置仍然存在。
	if err := engine.store.Prepare(project); err != nil {
		return story.Genesis{}, err
	}

	engine.emit("initialize", "开始初始化故事")
	defer func() { engine.logOutcome("initialize", err) }()

	// Architect 负责将用户创意转为作品核心与初始状态，生成参数统一采用 architect 角色配置。
	input, err := asPrettyJSON(architectInput{UserIdea: project.Idea, TargetLength: story.LengthGoal(project.LengthProfile)})
	if err != nil {
		return story.Genesis{}, err
	}
	genesis, err = generateJSON[story.Genesis](ctx, engine, "architect", llm.RoleArchitect, "architect", "genesis", input)
	if err != nil {
		return story.Genesis{}, err
	}

	// 先保存候选结果供失败诊断，再由存储层校验并提交初始检查点；候选文件不代表初始化成功。
	if err := engine.store.SaveWorking(0, "genesis.json", genesis); err != nil {
		return story.Genesis{}, err
	}

	// Architect 的世界事实和人物内部条目可留空 ID，由程序统一生成稳定标识。
	// 先保存模型原始输出再补 ID，既保留故障证据，也保证正式 checkpoint 从一开始就可被后续 Patch 精确引用。
	genesis.InitialStoryState, err = story.ResolveInitialStateItemIDs(genesis.InitialStoryState)
	if err != nil {
		return story.Genesis{}, err
	}
	if err := engine.store.CommitGenesis(genesis); err != nil {
		return story.Genesis{}, err
	}

	// 只有初始事务提交成功才通知可续写，后续章节以这份正式状态为起点。
	engine.emit("architect", "作品核心、初始事实与当前方向已建立")
	return genesis, nil
}
