// Package story defines the persistent domain model for the novel-writing agent.
//
// The project deliberately stores only durable creative intent and observable story state.
// It does not encode scene counts, chapter lengths, mandatory turns, or a fixed
// chapter total: those are writing choices, not continuity facts.
package story

import "time"

type Project struct {
	Name          string    `json:"name"`
	Idea          string    `json:"idea"`
	LengthProfile string    `json:"length_profile,omitempty"` // A scale hint, never a chapter/word quota.
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	MaxCalls      int       `json:"max_calls"`
	CreatedAt     time.Time `json:"created_at"`
}

// Genesis is the architect's one-time output. Later chapters use the outline
// directly; no chapter-level director stands between the outline and writer.
type Genesis struct {
	Bible              StoryBible   `json:"bible"`
	Outline            StoryOutline `json:"outline"`
	InitialState       InitialState `json:"initial_state"`
	InitialReaderState ReaderState  `json:"initial_reader_state"`
}

// StoryBible 固化“这是哪一本书”：人物身份、目标读者、叙事承诺、
// 正典规则、风格和唯一的结局方向。它初始化后不随章节重规划。
type StoryBible struct {
	Title            string           `json:"title"`
	Genre            string           `json:"genre"`
	Logline          string           `json:"logline"`
	TargetReader     TargetReader     `json:"target_reader"`
	NarrativePromise NarrativePromise `json:"narrative_promise"`
	EndingDirection  string           `json:"ending_direction"`
	ProtagonistID    string           `json:"protagonist_id"`
	Style            StyleGuide       `json:"style"`
	Characters       []Character      `json:"characters"`
	CanonRules       []string         `json:"canon_rules"`
	RecurringMotifs  []string         `json:"recurring_motifs"`
}

// TargetReader must resemble one imaginable person with particular tastes.
// Broad market labels cannot tell the writer what this reader will actually enjoy.
type TargetReader struct {
	Name           string   `json:"name"`
	ReadingHistory string   `json:"reading_history"`
	Craves         []string `json:"craves"`
	Forgives       []string `json:"forgives"`
	DropsWhen      []string `json:"drops_when"`
	BingeTriggers  []string `json:"binge_triggers"`
	FitWithStory   string   `json:"fit_with_story"`
}

// NarrativePromise 描述这本书承诺提供的阅读体验，用于创作取舍与
// Reader 观察；它不是可以由程序按关键词判真的类型分类器。
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

// StoryOutline plans dramatic movement without assigning it to chapter slots.
type StoryOutline struct {
	Version           int             `json:"version"`
	CoreConflict      string          `json:"core_conflict"`
	ProtagonistDrive  string          `json:"protagonist_drive"`
	CurrentMovementID string          `json:"current_movement_id"`
	Movements         []StoryMovement `json:"movements"`
}

// StoryMovement 是可自然完成的一段戏剧运动，不对应固定章节区间。
type StoryMovement struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	DramaticPressure  string   `json:"dramatic_pressure"`
	IntendedChange    string   `json:"intended_change"`
	ExpectedReward    string   `json:"expected_reward"`
	CompletionSignals []string `json:"completion_signals"`
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

// InitialState 只保存正文开始前已经成立的客观状态。
type InitialState struct {
	Characters []CharacterState  `json:"characters"`
	Facts      []Fact            `json:"facts"`
	Threads    []PlotThreadState `json:"threads"`
}

// State 是 HEAD 指向章节的完整正典快照；Writer 可以读取，Reader 不可读取。
type State struct {
	Chapter              int               `json:"chapter"`
	Characters           []CharacterState  `json:"characters"`
	Facts                []Fact            `json:"facts"`
	Threads              []PlotThreadState `json:"threads"`
	Timeline             []TimelineEvent   `json:"timeline"`
	Summaries            []ChapterSummary  `json:"summaries"`
	OutlineVersion       int               `json:"outline_version"`
	CompletedMovementIDs []string          `json:"completed_movement_ids"`
	OutlineProgress      OutlineProgress   `json:"outline_progress"`
	StoryStatus          string            `json:"story_status"` // ongoing, ending, completed.
}

// OutlineProgress 记录当前 movement 的事实进度，由 Recorder 根据正文更新。
type OutlineProgress struct {
	CurrentMovementID string `json:"current_movement_id"`
	Status            string `json:"status"` // ongoing, completed, blocked.
	Evidence          string `json:"evidence"`
}

// CharacterState 保存人物当前动态及其知识边界；Character 中的稳定设定仍以 Bible 为准。
type CharacterState struct {
	CharacterID   string         `json:"character_id"`
	Goal          string         `json:"goal"`
	Emotion       string         `json:"emotion"`
	Location      string         `json:"location"`
	Knowledge     []Knowledge    `json:"knowledge"`
	Relationships []Relationship `json:"relationships"`
}

type Knowledge struct {
	FactID     string `json:"fact_id"`
	Belief     string `json:"belief"`
	Confidence int    `json:"confidence"`
}

type Relationship struct {
	TargetID string `json:"target_id"`
	Stance   string `json:"stance"`
	Trust    int    `json:"trust"`
	Tension  int    `json:"tension"`
	Note     string `json:"note"`
}

type Fact struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Visibility   string `json:"visibility"`
	SinceChapter int    `json:"since_chapter"`
}

type PlotThreadState struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	Progress           string `json:"progress"`
	OpenedChapter      int    `json:"opened_chapter"`
	LastTouchedChapter int    `json:"last_touched_chapter"`
}

type TimelineEvent struct {
	Chapter      int      `json:"chapter"`
	Description  string   `json:"description"`
	Participants []string `json:"participants"`
}

type ChapterSummary struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	KeyChanges []string `json:"key_changes"`
}

// StateDelta is the recorder's factual account of what the prose actually changed.
type StateDelta struct {
	Chapter         int               `json:"chapter"`
	Summary         ChapterSummary    `json:"summary"`
	NewFacts        []Fact            `json:"new_facts"`
	CharacterStates []CharacterState  `json:"character_states"`
	ThreadStates    []PlotThreadState `json:"thread_states"`
	NewThreads      []PlotThreadState `json:"new_threads"`
	TimelineEvents  []TimelineEvent   `json:"timeline_events"`
	OutlineProgress OutlineProgress   `json:"outline_progress"`
	StoryStatus     string            `json:"story_status"`
}

// CanonReview is a safety gate, not a literary taste score.
type CanonReview struct {
	Passed               bool     `json:"passed"`
	Issues               []Issue  `json:"issues"`
	RevisionInstructions []string `json:"revision_instructions"`
}

type Issue struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	Suggestion  string `json:"suggestion"`
}

// ReaderState is intentionally subjective and reader-visible. It contains no
// secret outline/canon knowledge that could contaminate the simulated reader.
type ReaderState struct {
	Chapter            int      `json:"chapter"`
	CaresAbout         []string `json:"cares_about"`
	UnderstandsGoal    string   `json:"understands_goal"`
	WantsNext          []string `json:"wants_next"`
	CurrentFeeling     string   `json:"current_feeling"`
	ReceivedPayoff     []string `json:"received_payoff"`
	ConfusedBy         []string `json:"confused_by"`
	LosingPatienceWith []string `json:"losing_patience_with"`
	ContinueReason     string   `json:"continue_reason"`
	SuggestedAction    string   `json:"suggested_action"` // continue, adjust, replan.
}
