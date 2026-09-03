# story-emerge

面向约 8～10 万字中篇的中文小说生成系统。通过明确的章节意图、正文验收和当前事实，持续从已有故事中产生后续。

    初始化：Story Architect → Story Core + Initial Story State + Current Direction
    每章：Chapter Planner → Writer → Editor → Commit → 原子提交
                                  ↓           ↓
                            退回 Writer    提取当前事实、轨迹、摘要
                            或 Planner

Planner 决定值得产生的叙事效果，Writer 自由创造实现路径，Editor 决定正文准入与完结，Commit 只提取已接受正文。没有 Reader、固定 Outline、Live Tension 或自动接受兜底。

## 启动

`go.mod` 声明 Go 1.27。配置五个模型职责后启动：

    cp config.example.yaml config.yaml
    # 填写供应商密钥以及 architect / planner / writer / editor / commit 的模型
    go run ./cmd/story-emerge

浏览器打开 `http://127.0.0.1:8787`，填写故事想法和篇幅。原始输入完整保存；Writer 不读取 User Idea。

Web 提供最多三章试读，已提交章节立即可读；之后可以续写下一章或持续生成到正式完结。短篇提前完成时按实际结果显示。

    go run ./cmd/story-emerge serve --config config.yaml --addr 127.0.0.1:8787
    go run ./cmd/story-emerge run --project novels/demo --chapters 3
    go run ./cmd/story-emerge status --project novels/demo
    go run ./cmd/story-emerge export --project novels/demo

`--chapters 0` 表示持续写到 Editor 确认完结，仍受项目模型调用预算约束。篇幅档位只提供全书软目标，不限制章节数或单章字数。用户明确指定的篇幅优先；8～10 万字以外的质量不属于本轮验证结论。

## 数据与模型配置

`config.yaml` 中的 providers 保存连接信息，roles 分别选择模型；配置不进入小说目录。真实配置已被 `.gitignore` 排除。配置改变后重启 Web，CLI 下一次运行会重新读取。

    novels/<name>/
    ├── project.json         原始 User Idea、书名、篇幅档位、调用预算
    ├── story-core.json      一次性作品核心
    ├── HEAD                 最新完整提交的章节号
    ├── chapters/            已接受正文
    ├── commits/             最终计划、Editor 判断、事实补丁、轨迹和短摘要
    ├── checkpoints/         当前事实、方向、最近五章轨迹、字符数、完结标记
    ├── .work/               规划尝试、草稿、审核和失败输出
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

行为测试覆盖角色输入边界、修订回退、空事实补丁、状态替换删除、轨迹窗口、提交失败恢复及完结停止。这些测试使用脚本化模型，不证明生成小说的文学质量。

设计与边界见 [docs/design.md](docs/design.md)，故障处理见 [恢复手册](project_cognition/runbooks/model-output-failure-recovery.md)。
