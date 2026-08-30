package workflow

import (
	"testing"

	"story_emerge/internal/story"
)

func TestEditorInterventionBudgetCountsOnlyCreativeInterventions(t *testing.T) {
	// 场景：Editor 先 accept；另一次技术格式重试发生在预算对象之外；
	// 随后有效的 revise 和 replan 各发生一次。
	// 预期：accept 与技术重试都不消耗预算，两种文学干预共享同一个两次上限。
	budget := newInterventionBudget()

	if err := budget.Consume(story.EditorAccept); err != nil {
		t.Fatal(err)
	}
	if budget.Remaining() != 2 {
		t.Fatalf("accept 消耗了预算: %d", budget.Remaining())
	}

	// 技术重试不调用 Consume；这里直接核对预算仍保持不变。
	if budget.Remaining() != 2 {
		t.Fatalf("技术重试改变了文学预算: %d", budget.Remaining())
	}

	if err := budget.Consume(story.EditorRevise); err != nil {
		t.Fatal(err)
	}
	if err := budget.Consume(story.EditorReplan); err != nil {
		t.Fatal(err)
	}
	if budget.Remaining() != 0 {
		t.Fatalf("两次干预后预算未耗尽: %d", budget.Remaining())
	}
}

func TestEditorCannotInterveneAfterBudgetExhaustion(t *testing.T) {
	// 场景：一章已经使用两次 revise/replan 机会。
	// 预期：第三次文学干预被确定性拒绝，工作流只能自动接受并 finalize。
	budget := newInterventionBudget()
	_ = budget.Consume(story.EditorRevise)
	_ = budget.Consume(story.EditorReplan)

	if err := budget.Consume(story.EditorRevise); err == nil {
		t.Fatal("预算耗尽后仍允许 Editor 干预")
	}
}
