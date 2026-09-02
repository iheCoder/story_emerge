# Novel Agent V2：动态状态驱动的中篇生成

本实现依据本次设计讨论落地，目标为约 8～10 万字中篇。保留 Writer 不读取 User Idea 的边界，暂不加入“本章必要上下文”、向量记忆或历史片段补充机制。

## 创作信息

- Story Core 只保留 Story Engine（循环与累积方向）、Reader Promises（承诺与真正兑现的方式）、Experience Contract（体验、表达原则、漂移边界）。初始化后不自动修改。
- Current Story State 分为世界事实、人物事实/认知/承诺、人际关系。它保存当前仍有效的结果，不保存事件流水。知识或猜测必须归属于人物。
- Current Direction 只有 focus 和 desired_shift，由 Planner 持续保持或更新，不分配章节数或固定事件。
- Recent Trajectory 保留最近五章的 story_move 与 narrative_shape，如实描述文本承担的功能，不评分。允许“主要是认知推进”或“日常情绪承接”。
- Chapter Ledger 从全部已提交章节提取记录投影出章节号、标题、短摘要，不另建可能失配的全局可变文件。

原始 User Idea 原样存在 project.json，是最高创作授权。初始事实与用户希望未来发生的事情必须区分；未来要求不能提前成为当前事实。

## 职责与输入

| 节点 | 输入 | 交付与权限 |
|---|---|---|
| Story Architect | User Idea、全书篇幅目标 | 一次性 Core、初态、方向与书名 |
| Chapter Planner | 原始授权、Core、当前事实、方向、近期轨迹、全部 Ledger、上一章、篇幅进度、可能的退回原因 | KEEP/UPDATE 方向与本章 Intent；唯一的未来方向决策者 |
| Writer | Core、当前事实、Intent、上一章、全书篇幅目标、下一章编号 | 自主写场景、事件、对白和路径；不读取 User Idea、方向、轨迹或 Ledger |
| Editor | 原始授权、Core、当前事实、方向、轨迹、Intent、上一章、Draft、篇幅进度、已有 Ledger | 正文准入与完结确认；Ledger 只补足全书兑现判断，不另建上下文选择机制 |
| Commit | 旧当前事实与已接受正文 | 事实补丁、轨迹与短摘要；没有规划、评价或完结权限 |

修订 Writer 使用相同输入白名单，额外接收当前草稿和最多三个阻断问题。它不会收到 Editor 的完整输入。每个模型职责可独立配置供应商和模型。

## 每章的决策循环

Planner 先保持或更新 Direction，再输出 Chapter Intent：intended_effect、why_now、最多两条必要 constraints。意图描述叙事效果，允许局势变化，也允许关系、理解、气氛、日常和情绪承接。

Already-True Test 比较“本章想产生的效果是否已经实现”，区分世界事实、人物知道什么和读者体验到什么。信息在 User Idea 中出现，不代表人物或读者已经知道；开篇把设定写成可感知场景也有价值。

Writer 自主实现意图。Editor 检查连续性、章节价值、因果可信度及足以阻断正文成立的执行问题。“删除后主线仍能继续”只是辅助问题，不能自动否决安静章节。判断依据必须来自正文，不能依靠“加深关系”等标签。

Editor 返回 ACCEPT、REVISE_WRITER 或 RETURN_TO_PLANNER。每个意图最多允许两次 Writer 修订，第三稿仍须接受审核。未通过则回到 Planner，绝不自动接受。全项目调用预算与取消信号终止持续失败的循环，不增加另一套文学配额。

规划和修订期间一直使用同一份已提交事实。候选方向只有随最终接受的章节才生效；失败尝试不能改变事实、字数或完结状态。

## 当前状态与提交

每类事实集合使用稳定 ID 和 upsert/remove。upsert 对新 ID 创建条目，对已有 ID 完整替换当前值，覆盖 create/update/replace；remove 删除失效项。未触碰条目保留，人物认知数组不无限拼接。

同一补丁可以新增人物及其关系，或一起移除人物和失效关系；应用完三类集合后检查所有引用。空补丁合法。所有修改在副本上进行，校验失败不影响旧事实。

只有 ACCEPT 正文进入提取。程序核验提取字段、ID、引用与枚举，不通过规则猜测小说语义。严格 Schema 与本地结构验证一起防止缺字段被 Go 零值吞掉；缺少业务信息时直接失败，格式修复不能编造事实。

正式提交依次写章节正文、最终计划/验收/提取记录、下一份检查点，最后原子替换 HEAD。所有读取与导出受 HEAD 限制。Core 不在逐章输出或更新协议中。

## 篇幅与完结

Planner 得到全书目标及已提交正文字数；字符统计只计算正文汉字、字母和数字，不含标题、空白、标点。目标影响规划取舍，不触发程序化阶段切换或强制结束。

Planner 在 Direction/Intent 中决定收束，Writer 写出结果。Editor 结合既有 Ledger 和当前正文确认核心承诺是否按 payoff_shape 兑现、本书需要交代的结果是否有合适落点；证据不足不确认完结。开放结局允许保留余味和未知。

只有 ACCEPT 判断中的 story_complete 可以设置检查点 completed。Commit 没有这个输出字段；程序在整章提交成功后停止。没有“字数到了即完结”的分支。

## 删除与保留

生产代码已删除 Bible、Outline、Reader、Live Tension、Arc Review/Exit、旧状态和自动 Finalize。依赖这些接口的旧实验 Go 执行器一并删除，历史报告和小说产物不作为生产入口。

保留普通 Go 工作流、文件检查点、统一模型调用/用量链路和既有书架阅读界面。不迁移旧项目格式，不同时运行两套生产架构。
