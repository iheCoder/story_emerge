package workflow

import "testing"

func TestDeterministicChecksDoNotEnforceChapterLength(t *testing.T) {
	// 场景：Writer 有意写出只有标题和一句话的极短章。
	// 预期：程序只能检查标题与元文本，不能把字符数包装成“疑似截断”。
	chapter := "# 第1章 短章\n\n门开了。"
	issues := deterministicIssues(chapter)
	for _, issue := range issues {
		if issue.Code == "too_short" || issue.Code == "too_long" || issue.Code == "likely_truncated" {
			t.Fatalf("工作流不应包含字数门槛: %#v", issue)
		}
	}
}
