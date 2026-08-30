package story

import "testing"

func TestValidateProjectAcceptsLengthProfileWithoutFixedChapterCount(t *testing.T) {
	// 场景：Web 用户只选择“中篇”，没有接触具体章节数。
	// 预期：项目可以进入总导演阶段，由总导演在 12-18 章范围内决定结构。
	project := Project{
		Idea: "一段想被改写的命运", LengthProfile: "medium",
		ChapterMinChars: 1600, ChapterMaxChars: 2200, MaxCalls: 100,
	}
	if err := ValidateProject(project); err != nil {
		t.Fatalf("篇幅档位应当构成合法项目输入: %v", err)
	}
}

func TestValidateProjectRejectsUnknownLengthProfile(t *testing.T) {
	// 场景：调用方既没有固定章节数，也传入了未知篇幅值。
	// 预期：在模型调用前拒绝请求，避免总导演拿到没有边界的任务。
	project := Project{
		Idea: "一段想被改写的命运", LengthProfile: "epic",
		ChapterMinChars: 1600, ChapterMaxChars: 2200, MaxCalls: 100,
	}
	if err := ValidateProject(project); err == nil {
		t.Fatal("未知篇幅档位不应通过校验")
	}
}

func TestChapterRangeForLengthKeepsProductBoundariesStable(t *testing.T) {
	// 场景：前端的三个选择需要稳定映射为总导演的章节决策范围。
	// 预期：短、中、长篇分别得到明确但非单点的范围。
	tests := []struct {
		profile      string
		minimum, max int
	}{
		{profile: "short", minimum: 6, max: 8},
		{profile: "medium", minimum: 12, max: 18},
		{profile: "long", minimum: 24, max: 30},
	}
	for _, test := range tests {
		minimum, maximum, valid := ChapterRangeForLength(test.profile)
		if !valid || minimum != test.minimum || maximum != test.max {
			t.Fatalf("%s 的范围错误: %d-%d valid=%t", test.profile, minimum, maximum, valid)
		}
	}
}
