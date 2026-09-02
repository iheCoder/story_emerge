package workflow

import "story_emerge/internal/story"

// writerContext 是明确的输入白名单。初稿、修订都使用它，绝不序列化 Project 或检查点整体，
// 因为它们含有 User Idea、Direction、Trajectory 等 Writer 不应读取的信息。
type writerContext struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	ChapterIntent     story.ChapterIntent     `json:"chapter_intent"`
	PreviousChapter   string                  `json:"previous_chapter"`
	TargetLength      string                  `json:"target_length"`
	NextChapter       int                     `json:"next_chapter"`
}

// lengthProgress 只给负责全局取舍的 Planner 和负责完结确认的 Editor。
// TargetLength 为软目标；WrittenCharacters 来自已提交正文，不含任何未通过草稿。
type lengthProgress struct {
	TargetLength      string `json:"target_length"`
	WrittenCharacters int    `json:"written_characters"`
}

type plannerInput struct {
	UserIdea          string                  `json:"user_idea"`
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
	PreviousChapter   string                  `json:"previous_chapter"`
	LengthProgress    lengthProgress          `json:"length_progress"`
	NextChapter       int                     `json:"next_chapter"`
	PlanningFeedback  string                  `json:"planning_feedback,omitempty"`
}
