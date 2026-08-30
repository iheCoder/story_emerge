# story-emerge

story-emerge 是一个可恢复的长篇中文网络小说 Agent。它不把文学创作编译成 scene 清单，也不追求建立完整小说事实数据库；系统优先让故事持续好看，同时守住作品身份、主要方向和真正重要的长期状态。

    初始化：Architect → Story Bible + Story Spine + Target Reader + Outline
    每章：Writer → 基础校验 → Editor → Story Update → Reader → 原子提交
                         accept / revise / replan
                         每章最多两次文学干预

长期模型角色只有 Architect、Writer、Editor 和 Reader。重规划是 Architect 的按需模式；Reader 只观察，不控制工作流。

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

`length` 可取 `short`、`medium`、`long`、`epic`。它只表示全书规模，不会变成固定章数、单章字数或 scene 配额。

## Web

    go run ./cmd/story-emerge serve --provider auto --addr 127.0.0.1:8787

Web 创建后生成三章试读。每一章推进 HEAD 后立即可读；此后可以生成下一章，或让 Agent 持续写到 `story_status=completed`。

## 项目文件

    novels/<name>/
    ├── brief.md                 原始点子
    ├── story.json / story.md    Story Bible、Story Spine 与目标读者
    ├── project.json             模型、规模意图与调用预算
    ├── HEAD                     当前完整提交
    ├── outlines/                Current Arc 与 Story Tracks 的版本
    ├── chapters/                最终正文
    ├── story-updates/           每章长期状态变化
    ├── summaries/               独立的读者可见短期记忆
    ├── editor-reviews/          Editor 的有限干预轨迹
    ├── reader-observations/     每章独立读者观察
    ├── checkpoints/             精简 Story State 快照
    ├── .work/                   草稿与中断现场
    └── usage.jsonl              调用与 token 审计

正文、Story Update、摘要、Editor 轨迹、Reader Observation、检查点和可选新 Outline 全部写好后才原子替换 HEAD。

## 验证

    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go test ./...
    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go test -race ./...
    GOTOOLCHAIN=local GOCACHE=/tmp/story-emerge-go-cache go vet ./...

设计取舍见 [docs/design.md](docs/design.md)。
