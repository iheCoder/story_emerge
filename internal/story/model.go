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
	Focus        string `json:"focus"`
	DesiredShift string `json:"desired_shift"`
}

// ChapterIntent 允许人物、认知、情绪和读者体验上的贡献，不把每章任务化。
type ChapterIntent struct {
	IntendedEffect string   `json:"intended_effect"`
	WhyNow         string   `json:"why_now"`
	Constraints    []string `json:"constraints"`
}
type ChapterPlan struct {
	DirectionAction  string        `json:"direction_action"`
	CurrentDirection *Direction    `json:"current_direction"`
	ChapterIntent    ChapterIntent `json:"chapter_intent"`
}
type EditorAction string

const (
	EditorAccept EditorAction = "ACCEPT"
	EditorRevise EditorAction = "REVISE_WRITER"
	EditorReplan EditorAction = "RETURN_TO_PLANNER"
)

// Editor 只裁定正文准入和是否已经完结；输出中没有状态补丁或下一章计划。
type EditorDecision struct {
	Action         EditorAction `json:"action"`
	Reason         string       `json:"reason"`
	BlockingIssues []string     `json:"blocking_issues"`
	StoryComplete  bool         `json:"story_complete"`
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

// CommitResult 只提取已接受正文。章节号、标题、字数与完成状态由程序和 Editor 提供。
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

// ChapterCommit 将最终计划、验收和提取结果绑定到同一章；失败尝试只留在 .work。
type ChapterCommit struct {
	Chapter int            `json:"chapter"`
	Title   string         `json:"title"`
	Plan    ChapterPlan    `json:"plan"`
	Review  EditorDecision `json:"review"`
	Result  CommitResult   `json:"result"`
}

// State 是 HEAD 对应的检查点。Writer 只接收 Story，不读取方向、轨迹或全书进度。
type State struct {
	Chapter           int               `json:"chapter"`
	Story             CurrentStoryState `json:"current_story_state"`
	Direction         Direction         `json:"current_direction"`
	RecentTrajectory  []TrajectoryEntry `json:"recent_trajectory"`
	WrittenCharacters int               `json:"written_characters"`
	Completed         bool              `json:"completed"`
}

// LengthGoal 将现有产品篇幅选择解释成全书软目标，不分配章节数或单章字数。
// 用户在 User Idea 明确指定的篇幅优先，由 Architect/Planner 按原始授权理解。
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
