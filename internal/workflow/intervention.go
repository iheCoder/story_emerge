package workflow

import (
	"fmt"

	"story_emerge/internal/story"
)

const maxEditorInterventions = 2

// interventionBudget 把文学干预和技术重试分开计数。
// 只有一个已经通过结构校验的 revise/replan 决策会调用 Consume。
type interventionBudget struct {
	remaining int
}

func newInterventionBudget() interventionBudget {
	return interventionBudget{remaining: maxEditorInterventions}
}

func (budget interventionBudget) Remaining() int {
	return budget.remaining
}

func (budget *interventionBudget) Consume(action story.EditorAction) error {
	if action == story.EditorAccept {
		return nil
	}
	if action != story.EditorRevise && action != story.EditorReplan {
		return fmt.Errorf("不能计入干预预算的 Editor action: %s", action)
	}
	if budget.remaining == 0 {
		return fmt.Errorf("Editor 干预预算已经耗尽")
	}

	budget.remaining--
	return nil
}
