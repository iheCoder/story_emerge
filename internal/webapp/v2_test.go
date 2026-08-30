package webapp

import "testing"

func TestValidateCreateRequestAcceptsEpicWithoutChapterRange(t *testing.T) {
	if err := validateCreateRequest(CreateRequest{Idea: "长篇故事", Length: "epic"}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoredStatusUsesStoryStatusInsteadOfChapterCount(t *testing.T) {
	status, _ := restoredStatus(100, "ongoing")
	if status == "complete" {
		t.Fatal("写到 100 章不能代替故事自然完成状态")
	}
	status, _ = restoredStatus(7, "completed")
	if status != "complete" {
		t.Fatalf("已完成故事没有恢复为 complete: %s", status)
	}
}

func TestEpicCallBudgetAllowsLongRunningStory(t *testing.T) {
	if got := callBudget("epic"); got != 900 {
		t.Fatalf("epic 调用预算=%d", got)
	}
}
