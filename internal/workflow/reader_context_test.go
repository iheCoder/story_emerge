package workflow

import (
	"context"
	"testing"

	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

func TestReaderVisibleHistoryRetrievesOnlyRelevantEarlySummaries(t *testing.T) {
	// 场景：五章历史中，当前正文重新提到第一章的纸鹤，第二章与当前无关。
	// 预期：最近三章保持连续；早期窗口只召回有可见文本联系的第一章。
	summaries := []story.ChapterSummary{
		{Number: 1, Title: "纸鹤", Summary: "阿禾收到一只蓝色纸鹤"},
		{Number: 2, Title: "集市", Summary: "阿禾在集市购买干粮"},
		{Number: 3, Title: "渡口", Summary: "阿禾抵达渡口"},
		{Number: 4, Title: "夜雨", Summary: "众人在雨夜借宿"},
		{Number: 5, Title: "清晨", Summary: "旅店在清晨开门"},
	}

	recent, early := readerVisibleHistory("阿禾在窗边再次看见那只蓝色纸鹤", summaries)

	if len(recent) != 3 || recent[0].Number != 3 || recent[2].Number != 5 {
		t.Fatalf("最近摘要窗口错误: %#v", recent)
	}
	if len(early) != 1 || early[0].Number != 1 {
		t.Fatalf("早期检索混入无关摘要: %#v", early)
	}
}

func TestReaderVisibleHistoryDoesNotFillEarlyWindowWithoutEvidence(t *testing.T) {
	// 场景：当前正文和早期摘要没有共同文本。
	// 预期：早期窗口保持为空，不为了达到固定数量强行灌入历史。
	summaries := []story.ChapterSummary{
		{Number: 1, Summary: "海边旧屋"},
		{Number: 2, Summary: "山中夜雪"},
		{Number: 3, Summary: "河谷清晨"},
		{Number: 4, Summary: "城门钟声"},
	}

	_, early := readerVisibleHistory("沙漠烈日", summaries)
	if len(early) != 0 {
		t.Fatalf("无相关性时仍强行召回早期摘要: %#v", early)
	}
}

func TestWriterSummaryProjectionDoesNotMutateCanonicalHistory(t *testing.T) {
	// 场景：完整状态已经有五章摘要，而 Writer 只需要最近三章。
	// 预期：裁剪发生在副本上，作为提交基底的原状态仍保留全部摘要。
	full := story.State{Chapter: 5, Summaries: []story.ChapterSummary{
		{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5},
	}}
	projected := full
	projected.Summaries = recentSummaries(full.Summaries, recentSummaryLimit)

	if len(projected.Summaries) != 3 {
		t.Fatalf("Writer 摘要窗口错误: %#v", projected.Summaries)
	}
	if len(full.Summaries) != 5 || full.Summaries[0].Number != 1 {
		t.Fatalf("Writer 投影污染完整正典历史: %#v", full.Summaries)
	}
}

func TestRecorderAppliesDeltaToFullCanonicalHistory(t *testing.T) {
	// 场景：Writer 只看最近三章，但本章开始前已经提交五章摘要。
	// 预期：Recorder 应用第六章差量后保留全部六章，而不是只剩窗口加新章。
	full := story.State{Chapter: 5, StoryStatus: "ongoing", Summaries: numberedSummaries(5),
		OutlineProgress: story.OutlineProgress{CurrentMovementID: "m", Status: "ongoing"}}
	projected := full
	projected.Summaries = recentSummaries(full.Summaries, recentSummaryLimit)
	delta := story.StateDelta{Chapter: 6, Summary: story.ChapterSummary{Number: 6},
		OutlineProgress: story.OutlineProgress{CurrentMovementID: "m", Status: "ongoing"}, StoryStatus: "ongoing"}
	fake := &scriptedGenerator{inputs: map[string]string{}, responses: map[string]string{"chapter_006_record": mustJSON(delta)}}
	engine, err := New(fake, store.New(t.TempDir()), 5, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, next, err := engine.extractValidDelta(context.Background(), chapterWork{
		context: writerContext{State: projected, NextChapter: 6}, canonicalBase: full,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Summaries) != 6 || next.Summaries[0].Number != 1 {
		t.Fatalf("Recorder 用 Writer 投影覆盖了完整历史: %#v", next.Summaries)
	}
}

func numberedSummaries(count int) []story.ChapterSummary {
	summaries := make([]story.ChapterSummary, count)
	for index := range summaries {
		summaries[index].Number = index + 1
	}

	return summaries
}
