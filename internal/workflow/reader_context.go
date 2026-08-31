package workflow

import (
	"sort"
	"strings"
	"unicode"

	"story_emerge/internal/story"
)

const (
	earlyReaderRetrievalLimit = 3
	minimumSharedTerms        = 3
)

type scoredSummary struct {
	summary story.ChapterSummary
	score   int
}

// visibleHistory 把正文可见历史分成连续窗口和按需召回窗口。
// excludedNumber 对应已经以全文提供的章节；先排除其摘要，避免重复强调同一章。
func visibleHistory(query string, summaries []story.ChapterSummary, excludedNumber int) ([]story.ChapterSummary, []story.ChapterSummary) {
	eligible := summariesExcept(summaries, excludedNumber)
	recentStart := len(eligible) - recentSummaryLimit
	if recentStart < 0 {
		recentStart = 0
	}

	recent := append([]story.ChapterSummary(nil), eligible[recentStart:]...)
	early := retrieveEarlySummaries(query, eligible[:recentStart], earlyReaderRetrievalLimit)

	return recent, early
}

func summariesExcept(summaries []story.ChapterSummary, excludedNumber int) []story.ChapterSummary {
	result := make([]story.ChapterSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Number != excludedNumber {
			result = append(result, summary)
		}
	}

	return result
}

// retrieveEarlySummaries 仅召回与当前正文存在可见文本联系的早期摘要。
// limit 是上下文成本上限；没有文本联系时返回空集合，不为了填满窗口硬塞历史。
func retrieveEarlySummaries(chapter string, candidates []story.ChapterSummary, limit int) []story.ChapterSummary {
	queryTerms := retrievalTerms(chapter)
	if len(queryTerms) == 0 || limit < 1 {
		return []story.ChapterSummary{}
	}

	scored := scoreSummaries(queryTerms, candidates)
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].score == scored[right].score {
			return scored[left].summary.Number > scored[right].summary.Number
		}

		return scored[left].score > scored[right].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]story.ChapterSummary, 0, len(scored))
	for _, candidate := range scored {
		result = append(result, candidate.summary)
	}

	return result
}

func scoreSummaries(queryTerms map[string]struct{}, candidates []story.ChapterSummary) []scoredSummary {
	result := make([]scoredSummary, 0, len(candidates))
	for _, candidate := range candidates {
		score := sharedTermCount(queryTerms, retrievalTerms(summaryText(candidate)))
		// 少量双字符通常只是人物名及其相邻虚词，不足以证明早期情节相关。
		if score >= minimumSharedTerms {
			result = append(result, scoredSummary{summary: candidate, score: score})
		}
	}

	return result
}

func sharedTermCount(left, right map[string]struct{}) int {
	count := 0
	for term := range left {
		if _, exists := right[term]; exists {
			count++
		}
	}

	return count
}

func summaryText(summary story.ChapterSummary) string {
	return strings.Join([]string{summary.Title, summary.Summary, strings.Join(summary.KeyChanges, " ")}, " ")
}

// retrievalTerms 使用连续双字符作为与语言无关的轻量检索单位。
// 只保留字母和数字，避免 Markdown 标记与标点制造虚假匹配。
func retrievalTerms(text string) map[string]struct{} {
	normalized := make([]rune, 0, len(text))
	for _, character := range []rune(strings.ToLower(text)) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			normalized = append(normalized, character)
		}
	}

	terms := make(map[string]struct{})
	for index := 0; index+1 < len(normalized); index++ {
		terms[string(normalized[index:index+2])] = struct{}{}
	}

	return terms
}
