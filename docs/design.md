# 让故事持续生长，而不是维护完整事实数据库

## 1. 第一目标

系统首先要持续生成值得读下去的网络小说，并让主线、人物核心状态、阅读承诺和未来发展空间保持可理解。细枝末节的零漂移不是第一目标。

程序只保护章节号、ID、引用、枚举、文件完整性和 HEAD 事务等确定性不变量。情节是否自然、巧合是否过多、旧物是否曾经翻看等语义判断不伪装成程序校验。

## 2. 四个长期角色

- Architect 初始化 Story Bible、具体 Target Reader 和 Outline；收到 Editor 的 `replan` 后，只按需调整未来 Outline。
- Writer 读取作品身份、当前方向、精简长期状态、上一章、最近摘要和上一章 Reader Observation，自由决定具体写法。
- Editor 只在当前结果已经明显损害这本小说时干预，并从最终正文提炼 Story Update。
- Reader 只读取读者可见内容并形成独立观察；它不读取自己的历史评价，也不控制工作流。

不存在逐章 Director、Chapter Plan、Scene Plan、固定字数、固定转折或固定 Track 覆盖率。

## 3. Bible、Outline 与 State

Story Bible 回答“这究竟是哪一本小说”，保存 Premise、Story Spine、具体目标读者、Narrative Promise、人物、世界规则、稳定事实、结局方向和风格。普通重规划没有修改权限。

Outline 回答“当前有哪些力量正在向哪里发展”：

- Current Arc 表示当前阶段的整体变化及自然完成信号。
- Story Tracks 表示独立演化的力量。代码不知道它属于什么题材，也不要求 Writer 每章推进。
- Future Directions 保存尚未成为逐章任务的未来可能。

Story State 只回答“未来若忘掉会明显改变故事理解的当前状态”，包含人物长期现状、重要全局状态和 Track 实际进度。读者可见摘要独立存储，不让历史流水重新膨胀长期状态。

## 4. 一章的有限干预事务

    Writer 生成 Draft 1
        ↓
    基础程序校验
        ↓
    Editor: accept / revise / replan
        ↓
    revise 或 replan 共用两次 Intervention Budget
        ↓
    预算耗尽后最终 Draft 自动通过
        ↓
    Editor Finalize 只提炼 Story Update
        ↓
    Apply Story Update → Reader → 原子提交

`accept` 不消耗预算，并同时返回 Story Update 与读者可见摘要。`revise` 和 `replan` 各消耗一次；API 超时、JSON 修复和 Schema 错误不消耗文学预算。两次预算耗尽后 Editor 不再拥有否决权。

`replan` 是 `Architect(mode=replan)`：只能调整 Current Arc、Track 的未来 Direction/Status 和 Future Directions。输出结构中没有 Bible、Story Spine 或正文，因此无法偷改已提交历史。

## 5. Reader 单向边界

第 N 章的 Reader Observation 只进入第 N+1 章 Writer 和 Editor。第 N+1 次 Reader 只读取固定目标画像、上一章、已提交的读者可见摘要、必要的早期摘要检索和当前章。

这种单向边界避免 Reader 用自己的旧评价证明新评价。Reader 的困惑与期待是创作反馈，不是 accept、revise 或 replan 指令。

## 6. 事务与长篇边界

所有中间草稿和 Editor 决策先进入 `.work`。只有最终正文、Story Update、摘要、Editor 轨迹、Reader Observation、候选 Outline 和新 State 全部写入后，Store 才原子推进 HEAD。任何失败都从上一完整章节恢复。

精简状态与有界 Editor 解决的是上下文膨胀和无限质量循环，不等于已经证明百章吸引力。三章实跑只能发现明显退化；长篇能力仍需要多 seed、多 trial、真实读者反馈和几十章以上的状态增长观测。
