package workflow

import (
	"testing"

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

	recent, early := visibleHistory("阿禾在窗边再次看见那只蓝色纸鹤", summaries, 0)

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

	_, early := visibleHistory("沙漠烈日", summaries, 0)
	if len(early) != 0 {
		t.Fatalf("无相关性时仍强行召回早期摘要: %#v", early)
	}
}

func TestVisibleHistoryExcludesChapterAlreadyProvidedInFull(t *testing.T) {
	// 场景：第五章全文已经单独提供，同时摘要仓库也含第五章摘要。
	// 预期：最近窗口与早期检索都排除第五章，避免同一章在上下文里被重复加权。
	summaries := numberedSummaries(6)
	recent, early := visibleHistory("第五章全文", summaries, 5)

	for _, summary := range append(recent, early...) {
		if summary.Number == 5 {
			t.Fatalf("已提供全文的章节仍进入摘要窗口: recent=%#v early=%#v", recent, early)
		}
	}
}

func TestRecentSummariesReturnsIndependentWindow(t *testing.T) {
	// 场景：已经提交五份独立摘要，而 Writer 只读取最近三份。
	// 预期：返回窗口拥有独立底层数组，修改上下文不会污染持久化来源。
	full := numberedSummaries(5)
	window := recentSummaries(full, recentSummaryLimit)
	window[0].Number = 99

	if len(window) != 3 || full[2].Number != 3 {
		t.Fatalf("摘要窗口污染来源: full=%#v window=%#v", full, window)
	}
}

func numberedSummaries(count int) []story.ChapterSummary {
	result := make([]story.ChapterSummary, count)
	for index := range result {
		result[index].Number = index + 1
	}
	return result
}
