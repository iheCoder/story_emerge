// Package story 定义小说 Agent 的核心领域对象。
//
// 这些结构不是为了复刻文学理论，而是把长篇写作中最容易遗忘的事实显式保存下来：
// 谁是谁、谁知道什么、剧情线推进到了哪里，以及本章实际改变了什么。
package story

import "time"

// FormatVersion 是 JSON 产物格式版本；升级结构时通过它识别是否需要迁移，而不是猜测字段。
const FormatVersion = 1

// Project 保存一次小说生产任务中不会随章节变化的运行参数。
// API 密钥刻意不在这里出现，只允许从环境变量读取。
type Project struct {
	Version         int       `json:"version"`           // 持久化格式版本。
	Name            string    `json:"name"`              // 项目和导出书稿使用的名称。
	Idea            string    `json:"idea"`              // 用户原始创作点子，不在初始化后被模型改写。
	Provider        string    `json:"provider"`          // openai 或 deepseek。
	Model           string    `json:"model"`             // 创建项目时确定的模型名。
	TargetChapters  int       `json:"target_chapters"`   // 全书目标章节数。
	ChapterMinChars int       `json:"chapter_min_chars"` // 审核使用的最小可见字数。
	ChapterMaxChars int       `json:"chapter_max_chars"` // 审核使用的最大可见字数。
	MaxCalls        int       `json:"max_calls"`         // 项目生命周期内的逻辑调用预算。
	CreatedAt       time.Time `json:"created_at"`        // 便于审计项目何时建立。
}

// Genesis 是总导演在开写前交付的完整创作起点。
type Genesis struct {
	Bible        StoryBible   `json:"bible"`         // 全书静态创作契约。
	InitialState InitialState `json:"initial_state"` // 第 0 章动态快照素材。
}

// StoryBible 是全书的北极星。已经提交的章节可以改变人物处境，
// 但不能悄悄改写这里约定的读者承诺、世界规则和最终方向。
type StoryBible struct {
	Title           string      `json:"title"`            // 读者看到的书名。
	Genre           string      `json:"genre"`            // 类型承诺，例如都市/悬疑。
	Logline         string      `json:"logline"`          // 一句话冲突和卖点。
	ReaderPromise   string      `json:"reader_promise"`   // 读者期待被兑现的体验。
	Ending          string      `json:"ending"`           // 总导演声明的完整结局方向。
	ProtagonistID   string      `json:"protagonist_id"`   // 必须存在于 Characters。
	Style           StyleGuide  `json:"style"`            // 跨章节文风护栏。
	Arcs            []StoryArc  `json:"arcs"`             // 连续覆盖全书的阶段安排。
	Characters      []Character `json:"characters"`       // 静态人物白名单。
	CanonRules      []string    `json:"canon_rules"`      // 世界规则与不可违背事实。
	RecurringMotifs []string    `json:"recurring_motifs"` // 用于保持意象和风格统一。
}

// StyleGuide 把文风约束从提示词中抽成可复用数据，确保每章写作者看到同一套边界。
type StyleGuide struct {
	PointOfView string   `json:"point_of_view"` // 叙事视角。
	Tone        string   `json:"tone"`          // 语言与情绪基调。
	ProseRules  []string `json:"prose_rules"`   // 可执行的句式/节奏规则。
}

// StoryArc 描述一个有起点、目标和高潮的阶段性故事弧，供章节规划选择当前推进重点。
type StoryArc struct {
	ID           string `json:"id"`            // 阶段稳定 ID。
	Name         string `json:"name"`          // 人读的阶段名称。
	StartChapter int    `json:"start_chapter"` // 覆盖起始章节（含）。
	EndChapter   int    `json:"end_chapter"`   // 覆盖结束章节（含）。
	Goal         string `json:"goal"`          // 阶段必须完成的目标。
	Climax       string `json:"climax"`        // 阶段高潮或转折。
}

// Character 只保存不会频繁变化的人物底色；当前位置、目标和情绪属于 State。
type Character struct {
	ID       string `json:"id"`       // 跨章节稳定人物 ID。
	Name     string `json:"name"`     // 正文显示姓名。
	Role     string `json:"role"`     // 主角/伙伴/对手等叙事角色。
	Trait    string `json:"trait"`    // 不随章节轻易改变的底色。
	Desire   string `json:"desire"`   // 长期欲望。
	Weakness string `json:"weakness"` // 可制造冲突的弱点。
	Secret   string `json:"secret"`   // 需要按计划揭示的隐藏信息。
}

// InitialState 是第 0 章的世界快照。
type InitialState struct {
	Characters       []CharacterState  `json:"characters"`        // 每个正式人物的起始状态。
	Facts            []Fact            `json:"facts"`             // 初始正典事实。
	Threads          []PlotThreadState `json:"threads"`           // 第 1 章可推进的剧情线。
	DirectorGuidance []string          `json:"director_guidance"` // 总导演给写作者的第一阶段提示。
}

// State 是每章提交后生成的新快照。旧快照永久保留，便于回看和恢复。
type State struct {
	Chapter          int               `json:"chapter"`           // HEAD 对应的最后提交章节。
	Characters       []CharacterState  `json:"characters"`        // 当前人物动态快照。
	Facts            []Fact            `json:"facts"`             // 全部已提交正典事实。
	Threads          []PlotThreadState `json:"threads"`           // 全部剧情线及生命周期。
	Timeline         []TimelineEvent   `json:"timeline"`          // 按章节追加的事件历史。
	Summaries        []ChapterSummary  `json:"summaries"`         // 供上下文使用的章节记忆。
	DirectorGuidance []string          `json:"director_guidance"` // 最近一次阶段复盘指导。
}

// CharacterState 保存人物在当前章节后的动态状态；它与 Character 的静态底色分离。
type CharacterState struct {
	CharacterID   string         `json:"character_id"`  // 静态人物 ID，作为合并主键。
	Goal          string         `json:"goal"`          // 当前章节后的即时目标。
	Emotion       string         `json:"emotion"`       // 当前可写入正文的情绪状态。
	Location      string         `json:"location"`      // 当前场景位置。
	Knowledge     []Knowledge    `json:"knowledge"`     // 对事实的主观认知集合。
	Relationships []Relationship `json:"relationships"` // 对其他正式人物的有向关系。
}

// Knowledge 表示某人物对某事实的认知，不等同于事实本身，允许不同人物持有不同相信程度。
type Knowledge struct {
	FactID     string `json:"fact_id"`    // 指向 State.Facts 的稳定 ID。
	Belief     string `json:"belief"`     // 人物如何理解该事实。
	Confidence int    `json:"confidence"` // 主观确信程度，供写作时区分怀疑/确认。
}

// Relationship 是带信任、张力和备注的有向关系，TargetID 必须指向 Bible 中的正式人物。
type Relationship struct {
	TargetID string `json:"target_id"` // 关系指向的正式人物。
	Stance   string `json:"stance"`    // 当前立场，例如合作/敌对。
	Trust    int    `json:"trust"`     // 信任程度。
	Tension  int    `json:"tension"`   // 冲突张力。
	Note     string `json:"note"`      // 可直接供规划器使用的关系备注。
}

// Fact 是可被后续章节引用的离散事实；Visibility 区分公开、人物已知和读者未知等叙事层级。
type Fact struct {
	ID           string `json:"id"`            // 跨章节稳定主键。
	Description  string `json:"description"`   // 可被正文或知识条目引用的事实描述。
	Visibility   string `json:"visibility"`    // 读者/人物可见性标签。
	SinceChapter int    `json:"since_chapter"` // 首次成为正典的章节。
}

// PlotThreadState 是可追踪的剧情线状态，用稳定 ID 串联“开启—推进—回收”。
type PlotThreadState struct {
	ID                   string `json:"id"`                     // 剧情线稳定主键。
	Name                 string `json:"name"`                   // 人读的剧情线名称。
	Kind                 string `json:"kind"`                   // main/sub/relationship 等类别。
	Status               string `json:"status"`                 // active/dormant/resolved 生命周期。
	Progress             string `json:"progress"`               // 最近一次可观察推进。
	OpenedChapter        int    `json:"opened_chapter"`         // 剧情线首次开启章节。
	LastTouchedChapter   int    `json:"last_touched_chapter"`   // 最近被本章触碰的章节。
	PlannedPayoffChapter int    `json:"planned_payoff_chapter"` // 计划兑现节点。
}

// TimelineEvent 是按提交章节追加的历史事件，主要用于给规划器提供时间顺序和参与者线索。
type TimelineEvent struct {
	Chapter      int      `json:"chapter"`      // 事件发生/被确认的章节。
	Description  string   `json:"description"`  // 简短、不可歧义的事件描述。
	Participants []string `json:"participants"` // 参与人物 ID。
}

// ChapterSummary 是短小的章节记忆，供后续上下文快速回顾，避免每次塞入整本正文。
type ChapterSummary struct {
	Number     int      `json:"number"`      // 对应已提交章节号。
	Title      string   `json:"title"`       // 章节标题。
	Summary    string   `json:"summary"`     // 可放入下一章上下文的短摘要。
	KeyChanges []string `json:"key_changes"` // 人物/事实/剧情线的关键变化。
}

// ChapterPlan 是写作者执行前的可审计施工图：场景目标、障碍、转折、结果和收束钩子都在此声明。
type ChapterPlan struct {
	Number        int         `json:"number"`         // 必须等于当前 HEAD+1。
	Title         string      `json:"title"`          // 章节标题。
	Purpose       string      `json:"purpose"`        // 本章对全书的功能。
	ActiveThreads []string    `json:"active_threads"` // 本章主动推进的既有剧情线。
	Scenes        []ScenePlan `json:"scenes"`         // 至少三个有冲突的场景。
	Payoff        string      `json:"payoff"`         // 本章兑现的小目标。
	Hook          string      `json:"hook"`           // 结尾推动下一章的钩子。
	Forbidden     []string    `json:"forbidden"`      // 本章不能提前揭示的内容。
	TargetChars   int         `json:"target_chars"`   // writer 应瞄准的字数。
}

// ScenePlan 描述单个场景的因果闭环，防止章节只堆对话而没有冲突推进。
type ScenePlan struct {
	Order    int    `json:"order"`    // 场景顺序。
	Goal     string `json:"goal"`     // 场景中人物要达成的事。
	Obstacle string `json:"obstacle"` // 阻止目标的力量。
	Turn     string `json:"turn"`     // 信息/行动转折。
	Outcome  string `json:"outcome"`  // 场景结束时可观察的结果。
}

// StateDelta 只描述本章造成的变化，避免反复总结整个世界导致事实漂移。
type StateDelta struct {
	Chapter         int               `json:"chapter"`          // 必须是当前状态 + 1。
	Summary         ChapterSummary    `json:"summary"`          // 本章记忆摘要。
	NewFacts        []Fact            `json:"new_facts"`        // 本章首次确认的事实。
	CharacterStates []CharacterState  `json:"character_states"` // 正式人物的增量动态。
	ThreadStates    []PlotThreadState `json:"thread_states"`    // 既有剧情线更新。
	NewThreads      []PlotThreadState `json:"new_threads"`      // 本章新开的剧情线。
	TimelineEvents  []TimelineEvent   `json:"timeline_events"`  // 本章追加的事件。
}

// Review 保存编辑器的硬性门槛和软性质量意见；Passed=false 时章节不能进入 CommitChapter。
type Review struct {
	Passed               bool     `json:"passed"`                // 是否满足提交门槛。
	Score                int      `json:"score"`                 // 编辑器综合评分。
	HardIssues           []Issue  `json:"hard_issues"`           // 不修复就不能提交的问题。
	QualityIssues        []Issue  `json:"quality_issues"`        // 可选的文学质量建议。
	RevisionInstructions []string `json:"revision_instructions"` // 给修订模型的行动列表。
}

// Issue 是可定位的审核问题，Code 便于程序合并确定性检查，Evidence/Suggestion 便于人工修订。
type Issue struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	Suggestion  string `json:"suggestion"`
}

// ArcReflection 是阶段复盘的轻量结果，只写入下一阶段指导，不直接改写既有事实。
type ArcReflection struct {
	Diagnosis  string   `json:"diagnosis"`  // 对当前阶段节奏的判断。
	Priorities []string `json:"priorities"` // 下一阶段优先推进事项。
	Warnings   []string `json:"warnings"`   // 下一阶段应避免的偏航。
}
