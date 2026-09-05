# Novel Agent：按叙事尺度分工的中篇生成

本实现面向约 8～10 万字中文小说。系统不使用固定 Outline 或逐章 Planner，而是让 Architect、Director、Writer、Editor 与 Commit 分别处理整本身份、阶段方向、单章创作、阅读序列判断和事实记录。

## 创作信息

- Story Core 保存 Story Engine、Reader Promises 与 Experience Contract。它定义“这是一本什么小说”，初始化后没有逐章改写入口。
- Story Spine 保存全书尺度的关键变化及因果作用。每项只有 from、to、why_it_matters、exit_evidence，描述当前路径中的变化与成立依据；由 Architect 一次建立，后续只读，不是已发生的事实、大纲或逐项完成清单。
- Current Story State 保存当前仍有效的世界事实、人物事实/认知/承诺及关系，不保存事件流水。人物猜测必须归属于人物。
- Current Direction 由 current_position、focus、desired_shift、reader_expectation 组成。current_position 说明已建立什么、仍欠什么，是可纠正的阶段判断；其余字段描述未来若干章节共同工作的故事区域、希望形成的累积变化及读者期待。
- Recent Trajectory 保留最近五章的 story_move 与 narrative_shape，只记录实际怎样移动，不评价好坏。
- Chapter Ledger 从 HEAD 范围内的全部章节提交记录投影出章节号、标题和短摘要，不维护另一份可变全局文件。

User Idea 原样保存在 project.json，只进入一次性的 Story Architect。其他角色依赖 Architect 建立的 Core，不通过反复读取原始想法重新解释创作授权。

## 职责与输入

| 节点 | 输入 | 交付与权限 |
|---|---|---|
| Story Architect | User Idea、全书篇幅目标 | 一次性书名、Core、初始 Spine、初态和初始 Direction |
| Story Director | Core、Spine、当前事实、Direction、近期轨迹、全部 Ledger、最近两章正式正文、篇幅进度、可选 Editor 请求 | KEEP / ADJUST / REPLACE 阶段 Direction；Spine 只读；不规划下一章、不判断完结 |
| Writer | Core、当前事实、Direction、近期轨迹、上一章、全书篇幅目标、下一章编号 | 自主决定本章局部发展并写正文；不读取 User Idea 或 Ledger |
| Story Editor | Core、当前事实、Direction、近期轨迹、最近正文、Draft、篇幅进度、全部 Ledger | ACCEPT / REVISE_WRITER、可选阶段复查请求及完结确认；不规划下一章 |
| Commit | 旧当前事实、已接受正文 | 事实补丁、轨迹和短摘要；不读取 Direction，不评价、不规划 |

Writer 是 Local Planner + Prose Writer。Direction 是跨章战略导航，不是本章 checklist；Writer 无需在一章内完成 desired_shift，也无需同时触及所有 Core Promise。安静日常、陪伴、关系、气氛和情绪承接仍可形成有效章节贡献。

完整 Spine 只供 Architect 生成和 Director 使用，Writer、Editor、Commit 均不读取。阶段变化通过当前 Direction 传达；current_position 不包含未来阶段列表或终局预告，也不能覆盖正式事实、人物知情范围和正文。修订阶段保留相同边界。

Editor 分开判断 contribution、sequence 与 execution。它评价的是“本章放进最近故事序列后是否成立”，而不是是否完成整段 Direction。章节准入和 Direction 复查请求是两个正交维度：一章可以 ACCEPT，同时请求 Director 从下一章起调整阶段方向。

## 生产闭环

```text
Story Architect（一次）
        ↓
Core + Story Spine + Initial State + Current Direction
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
   └── 是 → Story Director → 正式 Direction → 下一章 Writer
```

正常每三个 ACCEPT 章节，在第 3、6、9……章 Commit 之后调用一次 Director。Editor 也可以在某一章 ACCEPT 时请求提前复查。Director 必须看到最新正式 State、Trajectory 与 Ledger，因此不能在 Commit 前运行。

Director 默认优先 KEEP。ADJUST 用于同一阶段内的有限修正；REPLACE 只用于阶段已经完成、失效，或继续保持会稳定制造重复、停滞或 Core 漂移的情况。Director 的 reason 只用于审计，不提供给 Writer。

Spine 的生成、定位和提示词设计依据见 [Story Spine](story-spine.md)。Director 先依据真实历史判断变化成立的程度，再对照 Spine。最近两章原文用来核对摘要可能省略的情绪、限制和反证；更早证据不足时保留不确定性，不按计划补造事实。

Spine 固定，Director 只输出 action、direction、reason。它依据历史判断变化是否成立，不能改写 Spine，也不能为了遵从参照强造事件或把欠缺的条件视为已经满足。Direction 可承接多项变化的重叠部分，没有阶段游标、HOLD/ADVANCE 状态机、完成比例或独立 Replanner。

Editor 只返回 ACCEPT 或 REVISE_WRITER。结构性实现问题仍由 Writer 在相同正式上下文下重新选择局部发展；系统不存在 RETURN_TO_PLANNER。持续失败由取消信号或全项目调用预算停止，预算耗尽不能自动接受正文。

## Direction 与章节提交边界

章节 Commit 与 Direction Review 是两类独立事务：

- Chapter Commit 写正文、Editor 判断、事实提取和下一份 checkpoint，最后推进 HEAD。它不能改变 Direction。
- Direction Review 在当前 HEAD 的 checkpoint 上更新 Direction、版本及最近复查章号，不推进 HEAD，也不改写 Spine、正文、事实、轨迹或 Ledger。Direction 版本只随 ADJUST/REPLACE 增加；所有动作都保留 Architect 的原始 Spine。

Director 在章节提交后失败时，该章节已经是正式历史，旧 Direction 仍然有效，固定 Spine 保持原样。下一次运行先从当前 HEAD 重试 Director；成功后才允许 Writer 写下一章。这样既不重写已接受正文，也不会跳过阶段判断。`.work` 只保存诊断产物，决定另存于 `direction-reviews/`，是否正式生效以 checkpoint 为准；不能以先写入的审计文件覆盖旧检查点。

## 当前事实与完结

每类事实集合使用稳定 ID 和 upsert/remove。upsert 创建或完整替换当前值，remove 删除失效项；未触碰条目保留。空补丁合法，所有修改在副本上完成，校验失败不改变旧状态。

Commit 提取在落盘前先补齐新增人物条目 ID，并在状态副本上校验完整 Patch。未知引用、更新/删除冲突等状态操作错误会触发一次 `commit_correction`：输入仍以旧事实和 ACCEPT 正文为证据，附带失败候选与具体校验错误。两次候选分别保留在 `.work`，纠正不成功则停止，不能自动丢弃操作或猜测 ID 来推进 HEAD。此纠正受同一模型调用预算与取消信号约束，不增加新的角色，也不提供跨运行的草稿恢复。

只有 ACCEPT 正文进入 Commit。严格 Schema 与本地校验负责字段、ID、引用、动作和提交边界，不把文学判断编码成分数或固定题材规则。

全书目标篇幅和已提交字符数只提供给 Director 与 Editor 作为取舍依据，不自动切换阶段或结束故事。只有 Editor 对 ACCEPT 正文给出的 story_complete 能确认完结；Director、Writer 与 Commit 都没有完结权限。

篇幅尽量符合，小说质量优先。沿用现有档位区间与正文字数，不增加硬上限、阶段字数配额或程序化收束。Architect 从源头减少可选复杂度；Director 提前考虑未兑现承诺和剩余空间。可以为了必要的铺垫、过渡、后果和余韵超出目标，也可以在目标下限之前自然结束；两者本身都不是 Editor 拒绝理由。不能把必要变化压成一句结论来省略叙事过程。

## 恢复与产品入口

Web 的“继续生长”不以三章试读完成为前提：项目已初始化、未完结且空闲时即可逐章生成。中断的未提交章重新从 Writer 开始；如果章节已经提交、只是 Director 失败，则先补做 Direction Review。

生产不迁移旧 Planner 格式，不同时运行两套架构。旧实验报告和小说产物可以保留作为历史证据，但不定义当前运行协议。

本版初始化与检查点要求 `story_spine` 和 Direction 的 `current_position`。缺失它们的旧项目不会自动补造规划或覆写历史；需要使用本版新建小说。工程测试验证数据与权限边界，不证明跨阶段正文或整部小说质量已经改善。
