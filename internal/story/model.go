// Package story 定义小说 Agent 的核心领域对象。
//
// 这些结构不是为了复刻文学理论，而是把长篇写作中最容易遗忘的事实显式保存下来：
// 谁是谁、谁知道什么、剧情线推进到了哪里，以及本章实际改变了什么。
package story

import "time"

const FormatVersion = 1

// Project 保存一次小说生产任务中不会随章节变化的运行参数。
// API 密钥刻意不在这里出现，只允许从环境变量读取。
type Project struct {
	Version         int       `json:"version"`
	Name            string    `json:"name"`
	Idea            string    `json:"idea"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	TargetChapters  int       `json:"target_chapters"`
	ChapterMinChars int       `json:"chapter_min_chars"`
	ChapterMaxChars int       `json:"chapter_max_chars"`
	MaxCalls        int       `json:"max_calls"`
	CreatedAt       time.Time `json:"created_at"`
}

// Genesis 是总导演在开写前交付的完整创作起点。
type Genesis struct {
	Bible        StoryBible   `json:"bible"`
	InitialState InitialState `json:"initial_state"`
}

// StoryBible 是全书的北极星。已经提交的章节可以改变人物处境，
// 但不能悄悄改写这里约定的读者承诺、世界规则和最终方向。
type StoryBible struct {
	Title           string      `json:"title"`
	Genre           string      `json:"genre"`
	Logline         string      `json:"logline"`
	ReaderPromise   string      `json:"reader_promise"`
	Ending          string      `json:"ending"`
	ProtagonistID   string      `json:"protagonist_id"`
	Style           StyleGuide  `json:"style"`
	Arcs            []StoryArc  `json:"arcs"`
	Characters      []Character `json:"characters"`
	CanonRules      []string    `json:"canon_rules"`
	RecurringMotifs []string    `json:"recurring_motifs"`
}

type StyleGuide struct {
	PointOfView string   `json:"point_of_view"`
	Tone        string   `json:"tone"`
	ProseRules  []string `json:"prose_rules"`
}

type StoryArc struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	StartChapter int    `json:"start_chapter"`
	EndChapter   int    `json:"end_chapter"`
	Goal         string `json:"goal"`
	Climax       string `json:"climax"`
}

// Character 只保存不会频繁变化的人物底色；当前位置、目标和情绪属于 State。
type Character struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Trait    string `json:"trait"`
	Desire   string `json:"desire"`
	Weakness string `json:"weakness"`
	Secret   string `json:"secret"`
}

// InitialState 是第 0 章的世界快照。
type InitialState struct {
	Characters       []CharacterState  `json:"characters"`
	Facts            []Fact            `json:"facts"`
	Threads          []PlotThreadState `json:"threads"`
	DirectorGuidance []string          `json:"director_guidance"`
}

// State 是每章提交后生成的新快照。旧快照永久保留，便于回看和恢复。
type State struct {
	Chapter          int               `json:"chapter"`
	Characters       []CharacterState  `json:"characters"`
	Facts            []Fact            `json:"facts"`
	Threads          []PlotThreadState `json:"threads"`
	Timeline         []TimelineEvent   `json:"timeline"`
	Summaries        []ChapterSummary  `json:"summaries"`
	DirectorGuidance []string          `json:"director_guidance"`
}

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
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Status               string `json:"status"`
	Progress             string `json:"progress"`
	OpenedChapter        int    `json:"opened_chapter"`
	LastTouchedChapter   int    `json:"last_touched_chapter"`
	PlannedPayoffChapter int    `json:"planned_payoff_chapter"`
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

type ChapterPlan struct {
	Number        int         `json:"number"`
	Title         string      `json:"title"`
	Purpose       string      `json:"purpose"`
	ActiveThreads []string    `json:"active_threads"`
	Scenes        []ScenePlan `json:"scenes"`
	Payoff        string      `json:"payoff"`
	Hook          string      `json:"hook"`
	Forbidden     []string    `json:"forbidden"`
	TargetChars   int         `json:"target_chars"`
}

type ScenePlan struct {
	Order    int    `json:"order"`
	Goal     string `json:"goal"`
	Obstacle string `json:"obstacle"`
	Turn     string `json:"turn"`
	Outcome  string `json:"outcome"`
}

// StateDelta 只描述本章造成的变化，避免反复总结整个世界导致事实漂移。
type StateDelta struct {
	Chapter         int               `json:"chapter"`
	Summary         ChapterSummary    `json:"summary"`
	NewFacts        []Fact            `json:"new_facts"`
	CharacterStates []CharacterState  `json:"character_states"`
	ThreadStates    []PlotThreadState `json:"thread_states"`
	NewThreads      []PlotThreadState `json:"new_threads"`
	TimelineEvents  []TimelineEvent   `json:"timeline_events"`
}

type Review struct {
	Passed               bool     `json:"passed"`
	Score                int      `json:"score"`
	HardIssues           []Issue  `json:"hard_issues"`
	QualityIssues        []Issue  `json:"quality_issues"`
	RevisionInstructions []string `json:"revision_instructions"`
}

type Issue struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	Suggestion  string `json:"suggestion"`
}

type ArcReflection struct {
	Diagnosis  string   `json:"diagnosis"`
	Priorities []string `json:"priorities"`
	Warnings   []string `json:"warnings"`
}
