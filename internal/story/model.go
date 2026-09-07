// Package story 将作品身份、当前事实、未来方向和近期轨迹分开保存。
// 章节号、字数与完结标记只是运行元数据，不参与人物事实或文学评分。
package story

import "time"

type Project struct {
	Name          string    `json:"name"`
	Title         string    `json:"title"`
	Idea          string    `json:"idea"`
	LengthProfile string    `json:"length_profile"`
	MaxCalls      int       `json:"max_calls"`
	CreatedAt     time.Time `json:"created_at"`
}

// Genesis 只由一次性的 Story Architect 生成。书名属于元数据，Core 只保留三个创作概念。
type Genesis struct {
	Title             string            `json:"title"`
	StoryCore         StoryCore         `json:"story_core"`
	StorySpine        []string          `json:"story_spine"`
	InitialStoryState CurrentStoryState `json:"initial_story_state"`
	CurrentDirection  Direction         `json:"current_direction"`
}

type StoryCore struct {
	StoryEngine        StoryEngine        `json:"story_engine"`
	ReaderPromises     []ReaderPromise    `json:"reader_promises"`
	ExperienceContract ExperienceContract `json:"experience_contract"`
}
type StoryEngine struct {
	Loop            string `json:"loop"`
	ProgressionAxis string `json:"progression_axis"`
}
type ReaderPromise struct {
	Promise     string `json:"promise"`
	PayoffShape string `json:"payoff_shape"`
}
type ExperienceContract struct {
	TargetExperience    string   `json:"target_experience"`
	NarrativePrinciples []string `json:"narrative_principles"`
	DriftBoundaries     []string `json:"drift_boundaries"`
}

// CurrentStoryState 保存此刻仍有效的结果。人物认知归人物所有，不混入客观世界事实。
type CurrentStoryState struct {
	World         []WorldFact         `json:"world"`
	Characters    []CharacterState    `json:"characters"`
	Relationships []RelationshipState `json:"relationships"`
}
type WorldFact struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// StateItem 是人物状态内部可独立更新的最小持久化条目。
// ID 只负责跨章节定位同一条当前事实，Value 才是 Writer 和各角色使用的小说内容。
type StateItem struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}
type CharacterState struct {
	ID                       string      `json:"id"`
	Name                     string      `json:"name"`
	Facts                    []StateItem `json:"facts"`
	KnowledgeAndBeliefs      []StateItem `json:"knowledge_and_beliefs"`
	CommitmentsAndIntentions []StateItem `json:"commitments_and_intentions"`
}
type RelationshipState struct {
	ID          string   `json:"id"`
	Characters  []string `json:"characters"`
	Description string   `json:"description"`
}
type Direction struct {
	// CurrentPosition 是依据正式历史作出的阶段判断，不是另一份世界事实或下一章任务。
	CurrentPosition   string `json:"current_position"`
	Focus             string `json:"focus"`
	DesiredShift      string `json:"desired_shift"`
	ReaderExpectation string `json:"reader_expectation"`
}

// DirectorAction 描述 Story Director 对当前阶段方向的处理方式。
// KEEP 保持阶段惯性，ADJUST/REPLACE 只改变跨越若干章节的方向，不规划下一章事件。
type DirectorAction string

const (
	DirectorKeep    DirectorAction = "KEEP"
	DirectorAdjust  DirectorAction = "ADJUST"
	DirectorReplace DirectorAction = "REPLACE"
)

// DirectorDecision 只维护跨越若干章节的方向，不修改 Architect 建立的 Core 或 Spine。
// 它没有 story_status，也不携带逐章意图，避免阶段角色取得完结权或退化为逐章规划器。
type DirectorDecision struct {
	Action    DirectorAction `json:"action"`
	Direction Direction      `json:"direction"`
	Reason    string         `json:"reason"`
}

// DirectionReview 是一次已经基于正式历史完成的阶段方向复查记录。
// AfterChapter 与 Version 都是系统审计元数据，不进入 Director 的故事判断输入。
type DirectionReview struct {
	AfterChapter int              `json:"after_chapter"`
	Version      int              `json:"version"`
	Decision     DirectorDecision `json:"decision"`
}

type EditorAction string

const (
	EditorAccept EditorAction = "ACCEPT"
	EditorRevise EditorAction = "REVISE_WRITER"
)

// EditorAssessment 强迫 Story Editor 分开说明章节贡献、序列效果和正文实现，
// 避免先形成笼统好恶，再把同一印象复制成多个评审维度。
type EditorAssessment struct {
	Contribution string `json:"contribution"`
	Sequence     string `json:"sequence"`
	Execution    string `json:"execution"`
}

// DirectionReviewRequest 与章节是否接收正交：一章可以成立，同时暴露最近若干章的阶段性问题。
type DirectionReviewRequest struct {
	Requested bool   `json:"requested"`
	Reason    string `json:"reason"`
}

// Editor 只裁定正文准入、是否请求阶段复查和是否已经完结；它不能修改事实或 Direction。
type EditorDecision struct {
	ChapterDecision EditorAction           `json:"chapter_decision"`
	Assessment      EditorAssessment       `json:"assessment"`
	BlockingIssues  []string               `json:"blocking_issues"`
	DirectionReview DirectionReviewRequest `json:"direction_review"`
	StoryComplete   bool                   `json:"story_complete"`
}

// CollectionPatch 用相同 ID 的完整当前值完成 create/update/replace，用 remove 删除失效项。
// 未触碰的条目保留；数组字段属于当前快照，不与旧数组持续拼接。
type CollectionPatch[T any] struct {
	Upsert []T      `json:"upsert"`
	Remove []string `json:"remove"`
}

// CharacterPatch 只描述一个人物本章实际变化的内部条目。
// 人物没有出现在补丁中，或某个内部条目 ID 没有被触碰，都表示继续保留旧值。
type CharacterPatch struct {
	ID                       string                     `json:"id"`
	Name                     string                     `json:"name"`
	Facts                    CollectionPatch[StateItem] `json:"facts"`
	KnowledgeAndBeliefs      CollectionPatch[StateItem] `json:"knowledge_and_beliefs"`
	CommitmentsAndIntentions CollectionPatch[StateItem] `json:"commitments_and_intentions"`
}
type StatePatch struct {
	World         CollectionPatch[WorldFact]         `json:"world"`
	Characters    CollectionPatch[CharacterPatch]    `json:"characters"`
	Relationships CollectionPatch[RelationshipState] `json:"relationships"`
}
type TrajectoryMove struct {
	StoryMove      string `json:"story_move"`
	NarrativeShape string `json:"narrative_shape"`
}
type TrajectoryEntry struct {
	Chapter int `json:"chapter"`
	TrajectoryMove
}

// CommitResult 保存旧状态与已接受正文对应的状态维护结果、本章轨迹和摘要。
// 规划只供取舍状态，不属于输出；章节号、标题、字数与完成状态由程序和 Editor 提供。
type CommitResult struct {
	StatePatch      StatePatch     `json:"state_patch"`
	TrajectoryEntry TrajectoryMove `json:"trajectory_entry"`
	ChapterSummary  string         `json:"chapter_summary"`
}
type LedgerEntry struct {
	Chapter int    `json:"chapter"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// ChapterCommit 将验收和事实提取绑定到同一份正文；失败尝试只留在 .work。
// Direction 不属于章节提交，由 Architect 初始化、Director 在提交后单独维护。
type ChapterCommit struct {
	Chapter int            `json:"chapter"`
	Title   string         `json:"title"`
	Review  EditorDecision `json:"review"`
	Result  CommitResult   `json:"result"`
}

// State 是 HEAD 对应的检查点。Direction 元数据只保证阶段复查可恢复且不会重复执行。
type State struct {
	Chapter int               `json:"chapter"`
	Story   CurrentStoryState `json:"current_story_state"`
	// StorySpine 逐项简述全书主要处境与转向，由 Architect 一次建立并固定保存。
	// 转向是否已成立由 Director 根据历史判断，不预存证明事件或阶段状态。
	StorySpine                    []string          `json:"story_spine"`
	Direction                     Direction         `json:"current_direction"`
	DirectionVersion              int               `json:"direction_version"`
	DirectionReviewedAfterChapter int               `json:"direction_reviewed_after_chapter"`
	RecentTrajectory              []TrajectoryEntry `json:"recent_trajectory"`
	WrittenCharacters             int               `json:"written_characters"`
	Completed                     bool              `json:"completed"`
}

// LengthGoal 将现有产品篇幅选择解释成全书软目标，不分配章节数或单章字数。
// 用户在 User Idea 明确指定的篇幅优先，由 Architect 按原始授权建立 Core，Director 维护阶段取舍。
func LengthGoal(profile string) string {
	switch profile {
	case "short":
		return "约 1～3 万字"
	case "medium":
		return "约 8～10 万字"
	case "long":
		return "约 15～20 万字"
	case "epic":
		return "约 30 万字或以上"
	default:
		return ""
	}
}
func (state State) Status() string {
	if state.Completed {
		return "completed"
	}
	return "ongoing"
}
