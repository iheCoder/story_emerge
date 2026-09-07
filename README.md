# story-emerge

面向约 8～10 万字中篇的中文小说生成系统。通过阶段方向、Writer 自主局部规划、正文验收和当前事实，持续从已有故事中产生后续。

    初始化：Story Architect → Story Core + Story Spine + Initial Story State + Current Direction
    每章：Writer → Story Editor → Commit → 原子提交
             ↑          ↓            ↓
             └── 修订正文       提取当前事实、轨迹、摘要
    阶段：每 3 个 ACCEPT 章节或 Editor 请求 → Story Director → Direction

Director 维护未来若干章节的阶段方向，Writer 自主决定本章最自然的局部发展并完成正文，Editor 判断本章放进最近序列后是否成立，Commit 根据旧状态和已接受正文维护当前状态，参考 Spine、Direction 和近期轨迹清理冗余历史，并生成本章轨迹与摘要。没有 Chapter Planner、Reader、固定 Outline、Live Tension 或自动接受兜底。

Spine 描述全书关键变化及因果作用，由 Architect 一次建立并固定保存；Director 根据正式事实、摘要与最近两章原文判断进展，只调整当前 Direction。Writer 不读取完整 Spine，只获得包含 current_position 的当前 Direction。篇幅尽量符合，小说质量优先，不因达标需要而跳过铺垫或强迫结局。

## 启动

`go.mod` 声明 Go 1.27。配置五个模型职责后启动：

    cp config.example.yaml config.yaml
    # 填写供应商密钥以及 architect / director / writer / editor / commit 的模型
    go run ./cmd/story-emerge

浏览器打开 `http://127.0.0.1:8787`，填写故事想法和篇幅。原始输入完整保存；Writer 不读取 User Idea。

Web 首次提供最多三章试读，已提交章节立即可读。只要已经初始化、尚未完结且没有正在运行的任务，故事页和最新章节末尾就提供“继续生长”，每次生成下一章；前三章中断后、刷新或服务重启后同样可用。三章之后还可选择持续生成到正式完结，短篇提前完成时按实际结果显示。

继续生长以最后一份正式 HEAD 为起点，保留已提交章节。未提交章节从 Writer 开始生成，不复用中断前的草稿或评审；如果章节已经提交、只是 Director 失败，则下一次运行先重试阶段复查，再写新章。

    go run ./cmd/story-emerge serve --config config.yaml --addr 127.0.0.1:8787
    go run ./cmd/story-emerge run --project novels/demo --chapters 3
    go run ./cmd/story-emerge status --project novels/demo
    go run ./cmd/story-emerge export --project novels/demo

`--chapters 0` 表示持续写到 Editor 确认完结，仍受项目模型调用预算约束。篇幅档位只提供全书软目标，不限制章节数或单章字数。用户明确指定的篇幅优先；8～10 万字以外的质量不属于本轮验证结论。

## 数据与模型配置

`config.yaml` 中的 providers 保存连接信息，roles 分别选择模型；配置不进入小说目录。真实配置已被 `.gitignore` 排除。配置改变后重启 Web，CLI 下一次运行会重新读取。

每个角色还可配置 `reasoning_effort` 和 `max_output_tokens`。省略时使用下表默认值；显式输出上限必须大于 0。可用推理档位为 none/minimal/low/medium/high/xhigh/max，具体模型需支持所选档位。

| 角色 | reasoning_effort | max_output_tokens |
|---|---|---:|
| architect | low | 16000 |
| director | low | 6000 |
| writer | none | 12000 |
| editor | low | 6000 |
| commit | none | 24000 |

    commit:
      provider: deepseek
      model: deepseek-v4-flash
      reasoning_effort: none
      max_output_tokens: 24000

正常生成采用角色配置；格式修复仍明确使用 none，并沿用该角色的输出上限。运行日志记录实际生效的参数。

    novels/<name>/
    ├── project.json         原始 User Idea、书名、篇幅档位、调用预算
    ├── story-core.json      一次性作品核心
    ├── HEAD                 最新完整提交的章节号
    ├── chapters/            已接受正文
    ├── commits/             Editor 判断、事实补丁、轨迹和短摘要
    ├── direction-reviews/   Director 的阶段方向复查记录，生效以 checkpoint 为准
    ├── checkpoints/         当前事实、Spine、方向及版本、最近五章轨迹、字符数、完结标记
    ├── .work/               草稿、审核、阶段判断原始结果和失败输出
    ├── runtime.log          追加式运行日志：阶段、调用结果、耗时、最终错误
    └── usage.jsonl          模型用量记录

Chapter Ledger 从 HEAD 范围内的 commits 派生。正文、提交记录和检查点全部写完后才替换 HEAD。失败留下的孤儿文件不会进入书架、上下文或书稿导出。

CLI 与 Web 都把每本小说的生成日志实时追加到 `runtime.log`，一行一条 JSON，重启后保留。它记录模型请求开始/成功/失败、输出预算、响应 ID、供应商返回的 token 统计，以及字段校验或提交失败等最终错误；不记录提示词和正文。不完整响应的已返回文本单独保存在 `.work/*-incomplete.txt`，不会被当成成功结果。`usage.jsonl` 仍只记录成功调用，不能用它排除失败尝试。

    tail -f novels/<name>/runtime.log

日志写入失败会明确报告到启动终端的 stderr。日志从新版本运行时开始产生，无法补回旧进程未持久化的历史错误。

## 验证与设计

    GOCACHE=/tmp/story-emerge-go-cache go test ./...
    GOCACHE=/tmp/story-emerge-go-cache go test -race ./...
    GOCACHE=/tmp/story-emerge-go-cache go vet ./...

行为测试覆盖角色输入边界、Spine 初始化/只读边界/重启恢复、规划整体提交失败、Writer 修订、三章周期与 Editor 提前触发、Director 故障恢复、空事实补丁、状态替换删除、轨迹窗口、软篇幅超出及完结停止。这些测试使用脚本化模型，不证明生成小说的文学质量。

本版要求新建小说生成字符串数组形式的 Spine；旧检查点缺少 `story_spine`、缺少 `current_position` 或仍使用四字段对象路线时明确拒绝，不自动转换或补写历史规划。设计说明见 [Story Spine](docs/story-spine.md)。

设计与边界见 [docs/design.md](docs/design.md)，故障处理见 [恢复手册](project_cognition/runbooks/model-output-failure-recovery.md)。
