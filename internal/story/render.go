package story

import "fmt"

// RenderStatus 每次从 HEAD 对应快照派生，避免失败提交提前覆盖独立的状态视图。
func RenderStatus(project Project, state State) string {
	return fmt.Sprintf("# %s\n\n已提交：第 %d 章\n\n正文字符：%d\n\n目标篇幅：%s\n\n状态：%s\n\n当前重心：%s\n\n期望变化：%s\n", project.Title, state.Chapter, state.WrittenCharacters, LengthGoal(project.LengthProfile), state.Status(), state.Direction.Focus, state.Direction.DesiredShift)
}
