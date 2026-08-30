# story-emerge

`story-emerge` 是一个用 Go 实现的状态化中文中篇小说 Agent。它把写小说当作可恢复的长期流程，而不是一次性要求模型“继续写到结局”。

V1 只有一个编排器和四个逻辑角色：

```text
总导演规划 → 小说家写作 → 书记员记账 → 编辑检查
      ↑                                  │
      └──── 不通过时定向修订一次 ─────────┘
                     └─ 二审仅剩单个局部硬伤时，可小修一次
```

四个角色都是可阅读的中文 Prompt，不是四个进程。项目不使用数据库、向量库、工作流框架或第三方 Go 依赖。

## 快速开始

需要 Go 1.24 或更高版本。

DeepSeek：

```bash
export DEEPSEEK_API_KEY="你的密钥"
go run ./cmd/story-emerge new \
  --idea-file examples/machine-last-ten-seconds.md \
  --out novels/machine-last-ten-seconds \
  --provider deepseek \
  --chapters 12

go run ./cmd/story-emerge run \
  --project novels/machine-last-ten-seconds
```

OpenAI：

```bash
export OPENAI_API_KEY="你的密钥"
go run ./cmd/story-emerge new \
  --idea-file examples/machine-last-ten-seconds.md \
  --out novels/openai-demo \
  --provider openai
```

默认模型分别是 `deepseek-v4-flash` 和 `gpt-5.6-luna`。DeepSeek V1 会拒绝其他模型名，避免实测时误用 Pro。

## Web 创作体验

Web 入口只向读者询问故事灵感和短篇/中篇/长篇。具体章节数由总导演在篇幅范围内根据故事决定。创建后先生成三章试读，每一章通过审核并推进 `HEAD` 后就会立即点亮并开放阅读。

```bash
export DEEPSEEK_API_KEY="你的密钥"
go run ./cmd/story-emerge serve --provider auto --addr 127.0.0.1:8787
```

打开 `http://127.0.0.1:8787`。三章试读结束后可以只生成下一章，也可以让工作流继续完成整个故事。后者仍然逐章提交，已完成章节无需等待全书结束即可阅读。

## 五个命令

- `new`：让总导演建立故事圣经、人物状态和剧情线账本。
- `run`：从 `HEAD` 继续逐章写作；`--chapters 1` 可只跑一章观察效果。
- `status`：不调用模型，直接查看当前人物和剧情线。
- `export`：只读取 `HEAD` 已提交历史，合并生成 `manuscript.md`。
- `serve`：启动本地 Web 入口，提供三章试读、沉浸阅读和后续生成。

完整参数可以通过 `go run ./cmd/story-emerge <命令> -h` 查看。

## 小说项目

```text
novels/<name>/
├── brief.md          原始点子
├── story.md          给人看的故事契约
├── status.md         当前人物和剧情线
├── manuscript.md     export 生成的完整书稿
├── story.json        机器读取的故事圣经
├── project.json      模型与目标配置，不含密钥
├── HEAD              当前有效章节号
├── chapters/         正文
├── plans/            逐章计划
├── deltas/           每章造成的状态变化
├── reviews/          编辑报告
├── checkpoints/      每章提交后的完整快照
├── .work/            未通过或中断时的现场
└── usage.jsonl       调用与 token 审计
```

提交时先写章节、计划、状态变化、评审和新检查点，最后才原子替换 `HEAD`。如果进程在中途退出，下一次仍从最后一个完整章节继续。

## 成本边界

- 每个项目默认最多 90 次逻辑模型调用。
- 每章最多完整修订一次；二审仅剩一个局部硬伤且篇幅合格时，可再小修一次。
- 每个阶段都有独立输出 token 上限。
- DeepSeek 的策划、正文与记账阶段关闭思考，编辑只使用低思考。
- `usage.jsonl` 记录实际输入、输出、缓存和总 token，不记录密钥。

## 验证

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go vet ./...
```

设计故事和取舍见 [docs/design.md](docs/design.md)。
