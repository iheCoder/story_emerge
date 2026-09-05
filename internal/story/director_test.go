package story

import (
	"reflect"
	"strings"
	"testing"
)

func testSpine() []SpineStage {
	return []SpineStage{{From: "临时相处", To: "主动信任", WhyItMatters: "共同选择需要信任", ExitEvidence: "愿意寻求支持，并让对方参与重要决定"}}
}

func TestEveryDirectionActionPreservesArchitectSpine(t *testing.T) {
	// 场景：同一份正式历史上分别保持、有限调整和替换当前方向。
	// 预期：所有动作仅影响 Direction 与复查元数据，Architect 的长期参照和正式历史始终不变。
	current := State{
		Chapter: 1, DirectionVersion: 1, WrittenCharacters: 100,
		Direction:        Direction{CurrentPosition: "初步信任仍待巩固", Focus: "共同生活", DesiredShift: "形成信任", ReaderExpectation: "能否自然依靠对方"},
		StorySpine:       testSpine(),
		RecentTrajectory: []TrajectoryEntry{{Chapter: 1, TrajectoryMove: TrajectoryMove{StoryMove: "愿意倾诉", NarrativeShape: "共同做饭"}}},
	}
	for _, action := range []DirectorAction{DirectorKeep, DirectorAdjust, DirectorReplace} {
		t.Run(string(action), func(t *testing.T) {
			direction, version := current.Direction, current.DirectionVersion
			if action == DirectorAdjust {
				direction.CurrentPosition = "愿意倾诉已成立，但共同决定仍欠支撑"
			}
			if action == DirectorReplace {
				direction.Focus = "信任对各自生活选择的影响"
				direction.DesiredShift = "开始让对方的需要进入自己的决定"
			}
			if action != DirectorKeep {
				version++
			}
			review := DirectionReview{AfterChapter: 1, Version: version, Decision: DirectorDecision{
				Action: action, Direction: direction, Reason: "第1章已有倾诉，尚未共同作出重要决定",
			}}
			next, err := ApplyDirectionReview(current, review)
			if err != nil {
				t.Fatal(err)
			}

			// 比较完整状态，只允许本次复查授权的三个字段变化。
			expected := current
			expected.Direction, expected.DirectionVersion = direction, version
			expected.DirectionReviewedAfterChapter = 1
			if !reflect.DeepEqual(next, expected) || !reflect.DeepEqual(current.StorySpine, testSpine()) {
				t.Fatalf("方向复查改变了长期参照或正式历史: next=%#v current=%#v", next, current)
			}
		})
	}
}

func TestValidateDirectorDecisionPreservesHealthyDirection(t *testing.T) {
	// 场景：阶段方向仍然健康，Director 选择 KEEP。
	// 预期：四个 Direction 字段必须原样返回，确保 Director 不会借 KEEP 偷偷改写阶段授权。
	current := Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立",
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
	current := Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "旧重心", DesiredShift: "旧变化", ReaderExpectation: "旧期待"}
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
	current := Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "重心", DesiredShift: "变化", ReaderExpectation: "期待"}
	tests := []struct {
		name     string
		decision DirectorDecision
		want     string
	}{
		{name: "missing reader expectation", decision: DirectorDecision{Action: DirectorReplace, Direction: Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "新重心", DesiredShift: "新变化"}, Reason: "旧阶段已经完成"}, want: "读者期待"},
		{name: "missing reason", decision: DirectorDecision{Action: DirectorReplace, Direction: Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "新重心", DesiredShift: "新变化", ReaderExpectation: "新期待"}}, want: "判断依据"},
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
