package workflow

import "story_emerge/internal/story"

// writerContext 是 Writer 的明确输入白名单。Writer 同时承担本章的局部规划与正文写作，
// 因此可以读取阶段 Direction 和近期事实轨迹，但仍不能读取 User Idea 或全书 Ledger。
type writerContext struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	PreviousChapter   string                  `json:"previous_chapter"`
	TargetLength      string                  `json:"target_length"`
	NextChapter       int                     `json:"next_chapter"`
}

// lengthProgress 只给负责阶段取舍的 Director 和负责完结确认的 Editor。
// TargetLength 为软目标；WrittenCharacters 来自已提交正文，不含任何未通过草稿。
type lengthProgress struct {
	TargetLength      string `json:"target_length"`
	WrittenCharacters int    `json:"written_characters"`
}

// chapterContext 汇集一章写作和评审共用的正式历史。具体角色仍从中投影自己的输入白名单，
// 避免 Writer 因复用 Editor 上下文而获得全书 Ledger 或其他超出局部创作所需的信息。
type chapterContext struct {
	StoryCore         story.StoryCore         `json:"story_core"`
	CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	CurrentDirection  story.Direction         `json:"current_direction"`
	RecentTrajectory  []story.TrajectoryEntry `json:"recent_trajectory"`
	ChapterLedger     []story.LedgerEntry     `json:"chapter_ledger"`
	PreviousChapter   string                  `json:"previous_chapter"`
	RecentChapters    []string                `json:"recent_chapters"`
	LengthProgress    lengthProgress          `json:"length_progress"`
	NextChapter       int                     `json:"next_chapter"`
}

// buildChapterContext 汇集下一章的正式历史，不读取 .work 中尚未提交的产物。
func (engine *Engine) buildChapterContext(project story.Project, core story.StoryCore, current story.State) (chapterContext, error) {
	// 上一章与 Ledger 都受 HEAD 约束，磁盘中的孤儿正文不能成为生成依据。
	previous, err := engine.store.LoadChapter(current.Chapter)
	if err != nil {
		return chapterContext{}, err
	}
	ledger, err := engine.store.LoadLedger()
	if err != nil {
		return chapterContext{}, err
	}

	recentChapters, err := engine.loadRecentChapters(current.Chapter)
	if err != nil {
		return chapterContext{}, err
	}

	return chapterContext{
		StoryCore: core, CurrentStoryState: current.Story,
		CurrentDirection: current.Direction, RecentTrajectory: current.RecentTrajectory,
		ChapterLedger: ledger, PreviousChapter: previous, RecentChapters: recentChapters, NextChapter: current.Chapter + 1,
		LengthProgress: lengthProgress{TargetLength: story.LengthGoal(project.LengthProfile), WrittenCharacters: current.WrittenCharacters},
	}, nil
}

// loadRecentChapters 给 Editor 和 Director 提供最近两章正式正文。
// 原文保留摘要容易省略的犹豫、限制和情绪铺垫；更早历史仍由 State 与 Ledger 提供。
// 始终通过 Store 的 HEAD 边界读取，不能把 .work 草稿或失败提交的孤儿文件当成阶段证据。
func (engine *Engine) loadRecentChapters(afterChapter int) ([]string, error) {
	chapters := make([]string, 0, 2)
	for number := max(1, afterChapter-1); number <= afterChapter; number++ {
		chapter, err := engine.store.LoadChapter(number)
		if err != nil {
			return nil, err
		}
		chapters = append(chapters, chapter)
	}
	return chapters, nil
}
