package story

import (
	"strings"
	"testing"
)

func TestValidateDirectorDecisionPreservesHealthyDirection(t *testing.T) {
	// 场景：阶段方向仍然健康，Director 选择 KEEP。
	// 预期：三个 Direction 字段必须原样返回，确保 Director 不会借 KEEP 偷偷改写阶段授权。
	current := Direction{
		Focus:             "关系在共同生活中逐渐变得可靠",
		DesiredShift:      "双方从临时合作走向主动依赖",
		ReaderExpectation: "这份依赖第一次承受现实压力时会如何变化",
	}
	decision := DirectorDecision{Action: DirectorKeep, Direction: current, Reason: "变化仍在累积，尚未形成阶段性事实"}

	if err := ValidateDirectorDecision(current, decision); err != nil {
		t.Fatalf("合法 KEEP 被拒绝: %v", err)
	}

	decision.Direction.Focus = "换一种更漂亮的说法"
	if err := ValidateDirectorDecision(current, decision); err == nil || !strings.Contains(err.Error(), "原样返回") {
		t.Fatalf("KEEP 改写方向未被拒绝: %v", err)
	}
}

func TestValidateDirectorDecisionRequiresARealStageChange(t *testing.T) {
	// 场景：Director 声称 ADJUST，却返回与当前阶段完全相同的内容。
	// 预期：动作和结果必须一致；真正改变方向后才允许通过。
	current := Direction{Focus: "旧重心", DesiredShift: "旧变化", ReaderExpectation: "旧期待"}
	decision := DirectorDecision{Action: DirectorAdjust, Direction: current, Reason: "需要有限修正"}
	if err := ValidateDirectorDecision(current, decision); err == nil || !strings.Contains(err.Error(), "没有改变") {
		t.Fatalf("空调整未被拒绝: %v", err)
	}

	decision.Direction.ReaderExpectation = "根据新状态形成的阶段期待"
	if err := ValidateDirectorDecision(current, decision); err != nil {
		t.Fatalf("有效调整被拒绝: %v", err)
	}
}

func TestValidateDirectorDecisionRejectsIncompleteOrUnknownOutput(t *testing.T) {
	// 场景：模型遗漏阶段读者期待、判断依据，或返回契约外动作。
	// 预期：领域校验在进入任何未来接线点前明确拒绝不完整决定。
	current := Direction{Focus: "重心", DesiredShift: "变化", ReaderExpectation: "期待"}
	tests := []struct {
		name     string
		decision DirectorDecision
		want     string
	}{
		{name: "missing reader expectation", decision: DirectorDecision{Action: DirectorReplace, Direction: Direction{Focus: "新重心", DesiredShift: "新变化"}, Reason: "旧阶段已经完成"}, want: "读者期待"},
		{name: "missing reason", decision: DirectorDecision{Action: DirectorReplace, Direction: Direction{Focus: "新重心", DesiredShift: "新变化", ReaderExpectation: "新期待"}}, want: "判断依据"},
		{name: "unknown action", decision: DirectorDecision{Action: DirectorAction("COMPLETE"), Direction: current, Reason: "越权决定完结"}, want: "action 无效"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDirectorDecision(current, test.decision)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("输出未按预期拒绝: err=%v want=%q", err, test.want)
			}
		})
	}
}
