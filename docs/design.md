# Novel Agent：按叙事尺度分工的中篇生成

本实现面向约 8～10 万字中文小说。系统不使用固定 Outline 或逐章 Planner，而是让 Architect、Director、Writer、Editor 与 Commit 分别处理整本身份、阶段方向、单章创作、阅读序列判断和事实记录。

## 创作信息

- Story Core 保存 Story Engine、Reader Promises 与 Experience Contract。它定义“这是一本什么小说”，初始化后没有逐章改写入口。
- Current Story State 保存当前仍有效的世界事实、人物事实/认知/承诺及关系，不保存事件流水。人物猜测必须归属于人物。
- Current Direction 由 focus、desired_shift、reader_expectation 组成，描述未来若干章节共同工作的故事区域、希望形成的累积变化及读者正在等待的阶段发展。
- Recent Trajectory 保留最近五章的 story_move 与 narrative_shape，只记录实际怎样移动，不评价好坏。
- Chapter Ledger 从 HEAD 范围内的全部章节提交记录投影出章节号、标题和短摘要，不维护另一份可变全局文件。

User Idea 原样保存在 project.json，只进入一次性的 Story Architect。其他角色依赖 Architect 建立的 Core，不通过反复读取原始想法重新解释创作授权。

## 职责与输入

| 节点 | 输入 | 交付与权限 |
|---|---|---|
| Story Architect | User Idea、全书篇幅目标 | 一次性书名、Core、初态和初始 Direction |
| Story Director | Core、当前事实、Direction、近期轨迹、全部 Ledger、篇幅进度、可选 Editor 请求 | KEEP / ADJUST / REPLACE 阶段 Direction；不规划下一章、不判断完结 |
| Writer | Core、当前事实、Direction、近期轨迹、上一章、全书篇幅目标、下一章编号 | 自主决定本章局部发展并写正文；不读取 User Idea 或 Ledger |
| Story Editor | Core、当前事实、Direction、近期轨迹、最近正文、Draft、篇幅进度、全部 Ledger | ACCEPT / REVISE_WRITER、可选阶段复查请求及完结确认；不规划下一章 |
| Commit | 旧当前事实、已接受正文 | 事实补丁、轨迹和短摘要；不读取 Direction，不评价、不规划 |

Writer 是 Local Planner + Prose Writer。Direction 是跨章战略导航，不是本章 checklist；Writer 无需在一章内完成 desired_shift，也无需同时触及所有 Core Promise。安静日常、陪伴、关系、气氛和情绪承接仍可形成有效章节贡献。

Editor 分开判断 contribution、sequence 与 execution。它评价的是“本章放进最近故事序列后是否成立”，而不是是否完成整段 Direction。章节准入和 Direction 复查请求是两个正交维度：一章可以 ACCEPT，同时请求 Director 从下一章起调整阶段方向。

## 生产闭环

```text
Story Architect（一次）
        ↓
Core + Initial State + Current Direction
        ↓
Writer（自主局部规划并写章）
        ↓
Story Editor
   ├── REVISE_WRITER → Writer 修订 → 再评审
   └── ACCEPT
          ↓
        Commit
          ├── Current Story State
          ├── Recent Trajectory
          └── Chapter Ledger
          ↓
满足阶段复查条件？
   ├── 否 → 下一章 Writer
   └── 是 → Story Director → 正式 Current Direction → 下一章 Writer
```

正常每三个 ACCEPT 章节，在第 3、6、9……章 Commit 之后调用一次 Director。Editor 也可以在某一章 ACCEPT 时请求提前复查。Director 必须看到最新正式 State、Trajectory 与 Ledger，因此不能在 Commit 前运行。

Director 默认优先 KEEP。ADJUST 用于同一阶段内的有限修正；REPLACE 只用于阶段已经完成、失效，或继续保持会稳定制造重复、停滞或 Core 漂移的情况。Director 的 reason 只用于审计，不提供给 Writer。

Editor 只返回 ACCEPT 或 REVISE_WRITER。结构性实现问题仍由 Writer 在相同正式上下文下重新选择局部发展；系统不存在 RETURN_TO_PLANNER。持续失败由取消信号或全项目调用预算停止，预算耗尽不能自动接受正文。

## Direction 与章节提交边界

章节 Commit 与 Direction Review 是两类独立事务：

- Chapter Commit 写正文、Editor 判断、事实提取和下一份 checkpoint，最后推进 HEAD。它不能改变 Direction。
- Direction Review 在当前 HEAD 的 checkpoint 上更新 Direction、版本号和最近复查章号，不推进 HEAD，也不改写正文、事实、轨迹或 Ledger。

Director 在章节提交后失败时，该章节已经是正式历史，旧 Direction 仍然有效。下一次运行先从当前 HEAD 重试 Director；成功后才允许 Writer 写下一章。这样既不重写已接受正文，也不会跳过阶段判断。`.work` 只保存诊断产物，正式决定另存于 `direction-reviews/` 并写入 checkpoint。

## 当前事实与完结

每类事实集合使用稳定 ID 和 upsert/remove。upsert 创建或完整替换当前值，remove 删除失效项；未触碰条目保留。空补丁合法，所有修改在副本上完成，校验失败不改变旧状态。

只有 ACCEPT 正文进入 Commit。严格 Schema 与本地校验负责字段、ID、引用、动作和提交边界，不把文学判断编码成分数或固定题材规则。

全书目标篇幅和已提交字符数只提供给 Director 与 Editor 作为取舍依据，不自动切换阶段或结束故事。只有 Editor 对 ACCEPT 正文给出的 story_complete 能确认完结；Director、Writer 与 Commit 都没有完结权限。

## 恢复与产品入口

Web 的“继续生长”不以三章试读完成为前提：项目已初始化、未完结且空闲时即可逐章生成。中断的未提交章重新从 Writer 开始；如果章节已经提交、只是 Director 失败，则先补做 Direction Review。

生产不迁移旧 Planner 格式，不同时运行两套架构。旧实验报告和小说产物可以保留作为历史证据，但不定义当前运行协议。
