// Package story 定义小说 Agent 的持久化领域模型。
//
// 模型只保存作品身份、未来方向和真正需要长期延续的当前状态。
// 章节结构、字数、场景数量和转折方式属于 Writer 的创作自由，不进入数据契约。
package story

import "time"

type Project struct {
	Name          string    `json:"name"`
	Idea          string    `json:"idea"`
	LengthProfile string    `json:"length_profile,omitempty"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	MaxCalls      int       `json:"max_calls"`
	CreatedAt     time.Time `json:"created_at"`
}

// Genesis 是 Architect 初始化模式的一次性交付。
type Genesis struct {
	Bible        StoryBible   `json:"bible"`
	Outline      StoryOutline `json:"outline"`
	InitialState InitialState `json:"initial_state"`
}

// StoryBible 回答“这究竟是哪一本小说”。普通重规划没有修改它的权限。
type StoryBible struct {
	Title            string           `json:"title"`
	Premise          string           `json:"premise"`
	StorySpine       string           `json:"story_spine"`
	TargetReader     TargetReader     `json:"target_reader"`
	NarrativePromise NarrativePromise `json:"narrative_promise"`
	Characters       []Character      `json:"characters"`
	WorldRules       []string         `json:"world_rules"`
	StableFacts      []string         `json:"stable_facts"`
	EndingDirection  string           `json:"ending_direction"`
	Style            StyleGuide       `json:"style"`
}

// TargetReader 必须像一个有具体经历和偏好的真人，而不是宽泛人口标签。
type TargetReader struct {
	Name           string   `json:"name"`
	ReadingHistory string   `json:"reading_history"`
	Craves         []string `json:"craves"`
	Forgives       []string `json:"forgives"`
	DropsWhen      []string `json:"drops_when"`
	BingeTriggers  []string `json:"binge_triggers"`
}

type NarrativePromise struct {
	PrimaryPleasure     string   `json:"primary_pleasure"`
	SupportingPleasures []string `json:"supporting_pleasures"`
	MustDeliver         []string `json:"must_deliver"`
	MustNotBecome       []string `json:"must_not_become"`
}

type StyleGuide struct {
	PointOfView string   `json:"point_of_view"`
	Tone        string   `json:"tone"`
	ProseRules  []string `json:"prose_rules"`
}

type Character struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Trait    string `json:"trait"`
	Desire   string `json:"desire"`
	Weakness string `json:"weakness"`
	Secret   string `json:"secret"`
}

// StoryOutline 只表达当前阶段和若干正在演化的力量，不分配逐章任务。
type StoryOutline struct {
	Version          int          `json:"version"`
	CurrentArc       StoryArc     `json:"current_arc"`
	Tracks           []StoryTrack `json:"tracks"`
	FutureDirections []string     `json:"future_directions"`
}

type StoryArc struct {
	Name              string   `json:"name"`
	Purpose           string   `json:"purpose"`
	CompletionSignals []string `json:"completion_signals"`
}

// StoryTrack 没有题材类型。Status 只表示这股力量是否仍需要继续发展。
type StoryTrack struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Direction string `json:"direction"`
	Status    string `json:"status"`
}

// InitialState 只保存正文开始前就必须长期维持的动态状态。
type InitialState struct {
	CharacterStates []CharacterState `json:"character_states"`
	DurableStates   []DurableState   `json:"durable_states"`
	TrackProgress   []TrackProgress  `json:"track_progress"`
}

// State 是 HEAD 指向的精简快照，不承担小说历史数据库职责。
type State struct {
	Chapter         int              `json:"chapter"`
	CharacterStates []CharacterState `json:"character_states"`
	DurableStates   []DurableState   `json:"durable_states"`
	TrackProgress   []TrackProgress  `json:"track_progress"`
	OutlineVersion  int              `json:"outline_version"`
	StoryStatus     string           `json:"story_status"`
}

// CharacterState 用一段当前态描述承载真正需要长期记住的人物变化。
// 它刻意不拆成知识图谱、关系图或物品图，避免状态重新膨胀。
type CharacterState struct {
	CharacterID string `json:"character_id"`
	State       string `json:"state"`
}

type DurableState struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type TrackProgress struct {
	TrackID  string `json:"track_id"`
	Progress string `json:"progress"`
}

// StoryUpdate 只描述最终正文造成的长期变化。三个 change 集合都允许为空。
type StoryUpdate struct {
	Chapter             int                    `json:"chapter"`
	CharacterChanges    []CharacterStateChange `json:"character_changes"`
	DurableStateChanges []DurableStateChange   `json:"durable_state_changes"`
	TrackChanges        []TrackProgressChange  `json:"track_progress_changes"`
	StoryStatus         string                 `json:"story_status"`
}

type CharacterStateChange struct {
	CharacterID string `json:"character_id"`
	State       string `json:"state"`
}

// DurableStateChange 使用 upsert/remove 表达有限、可验证的当前态变化。
type DurableStateChange struct {
	Operation   string `json:"operation"`
	ID          string `json:"id"`
	Description string `json:"description"`
}

type TrackProgressChange struct {
	TrackID  string `json:"track_id"`
	Progress string `json:"progress"`
}

// ChapterSummary 是正文的读者可见短期记忆，独立于长期 Story State。
type ChapterSummary struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	KeyChanges []string `json:"key_changes"`
}

type EditorAction string

const (
	EditorAccept EditorAction = "accept"
	EditorRevise EditorAction = "revise"
	EditorReplan EditorAction = "replan"
)

// EditorDecision 只判断当前结果是否严重到值得干预，不承担逐段导演职责。
type EditorDecision struct {
	Action           EditorAction   `json:"action"`
	Reason           string         `json:"reason"`
	RevisionGuidance string         `json:"revision_guidance"`
	ReplanGuidance   string         `json:"replan_guidance"`
	Observations     []string       `json:"observations"`
	StoryUpdate      StoryUpdate    `json:"story_update"`
	Summary          ChapterSummary `json:"reader_visible_summary"`
}

// EditorDecisionRecord 是正式历史需要保留的最小编辑轨迹。
// Story Update 和摘要各有自己的单一文件，不在这里重复保存。
type EditorDecisionRecord struct {
	Action           EditorAction `json:"action"`
	Reason           string       `json:"reason"`
	RevisionGuidance string       `json:"revision_guidance,omitempty"`
	ReplanGuidance   string       `json:"replan_guidance,omitempty"`
	Observations     []string     `json:"observations"`
}

// EditorFinalizeResult 在干预预算耗尽后只提炼状态，不再评价或阻止正文。
type EditorFinalizeResult struct {
	StoryUpdate StoryUpdate    `json:"story_update"`
	Summary     ChapterSummary `json:"reader_visible_summary"`
}

// EditorReviewLog 保存本章有限干预的可审计轨迹，不参与下一章创作上下文。
type EditorReviewLog struct {
	Chapter       int                    `json:"chapter"`
	Decisions     []EditorDecisionRecord `json:"decisions"`
	Interventions int                    `json:"interventions"`
	AutoAccepted  bool                   `json:"auto_accepted"`
}

// ReaderObservation 是目标读者对当前章的一次独立观察。
type ReaderObservation struct {
	Chapter            int      `json:"chapter"`
	CaresAbout         []string `json:"cares_about"`
	UnderstandsGoal    string   `json:"understands_goal"`
	MemorableMoments   []string `json:"memorable_moments"`
	WantsNext          []string `json:"wants_next"`
	CurrentFeeling     string   `json:"current_feeling"`
	ReceivedPayoff     []string `json:"received_payoff"`
	ConfusedBy         []string `json:"confused_by"`
	LosingPatienceWith []string `json:"losing_patience_with"`
	TurnPageReason     string   `json:"turn_page_reason"`
}
