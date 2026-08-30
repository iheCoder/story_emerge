# story-emerge

story-emerge 是一个可恢复的长篇中文网络小说 Agent。它不把文学创作编译成 scene 清单，而是让作者在正典和全书方向内自由写作，并让一个具体目标读者在每章后独立反馈阅读体验。

    初始化：Architect → 目标读者 + 叙事承诺 + 大纲 + 正典初态
    每章：Writer → Recorder → Canon Checker → Reader → 原子提交
            ↑          正典失败时仅完整修订一次
    Replanner 只在当前故事运动完成或阻塞时出现；Reader 不控制工作流。

这些角色都是中文 Prompt，不是独立进程。项目不使用数据库、向量库、工作流框架或第三方 Go 依赖。

## 快速开始

需要 Go 1.24 或更高版本。

    export DEEPSEEK_API_KEY="你的密钥"
    go run ./cmd/story-emerge new \
      --idea-file examples/machine-last-ten-seconds.md \
      --out novels/machine-last-ten-seconds \
      --provider deepseek \
      --length medium

    # 最多继续三章；传 0 则写到故事状态自然完成
    go run ./cmd/story-emerge run \
      --project novels/machine-last-ten-seconds \
      --chapters 3

length 可取 short、medium、long、epic。它只告诉 Architect 全书规模，不会变成固定章数、单章字数或 scene 配额。默认模型分别是 deepseek-v4-flash 和 gpt-5.6-luna。

## Web

    go run ./cmd/story-emerge serve --provider auto --addr 127.0.0.1:8787

Web 创建后生成三章试读。每一章推进 HEAD 后立即可读；此后可以生成下一章，或让 Agent 持续写到 story_status 为 completed。

## 项目文件

    novels/<name>/
    ├── brief.md                 原始点子
    ├── story.json / story.md    故事圣经、具体目标读者和叙事承诺
    ├── project.json             模型、规模意图与调用预算
    ├── HEAD                     当前完整提交
    ├── outlines/                初始化及触发式重规划版本
    ├── chapters/                正文
    ├── deltas/                  正文造成的事实变化
    ├── canon-reviews/           正典检查
    ├── reader-observations/     每章独立读者观察，只供下一章 Writer 参考
    ├── checkpoints/             完整正典快照
    ├── .work/                   未通过或中断的现场
    └── usage.jsonl              调用与 token 审计

正文、状态、Reader Observation 和可选新大纲全部写好后才原子替换 HEAD。中断后仍从最后一个完整章节继续。

## 验证

    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go test ./...
    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go test -race ./...
    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go vet ./...

设计取舍见 [docs/design.md](docs/design.md)，实现与精确输入回归见 [docs/implementation-and-regeneration-log.md](docs/implementation-and-regeneration-log.md)。
