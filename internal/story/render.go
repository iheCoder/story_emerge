package story

import (
	"fmt"
	"strings"
)

// RenderStory 把机器可校验的故事圣经投影成读者友好的 Markdown。
func RenderStory(bible StoryBible) string {
	// 这是 Bible 的确定性视图，不调用模型、不补写内容；同一份 Bible 始终得到同一份 Markdown。

	// 写入书名和一句话卖点，先让读者知道这本书讲什么。
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s\n\n", bible.Title)
	fmt.Fprintf(&builder, "> %s\n\n", bible.Logline)

	// 按 Bible 的业务区块输出类型、承诺和结局方向。
	writeSection(&builder, "类型", bible.Genre)
	writeSection(&builder, "目标读者", bible.TargetReader.Name+"："+bible.TargetReader.ReadingHistory)
	writeSection(&builder, "首要阅读快感", bible.NarrativePromise.PrimaryPleasure)
	writeSection(&builder, "最终方向", bible.EndingDirection)

	// 输出阶段、人物、正典规则和反复意象。
	writeCharacters(&builder, bible.Characters)
	writeList(&builder, "世界与正典规则", bible.CanonRules)
	writeList(&builder, "反复意象", bible.RecurringMotifs)

	// 最后输出全书文风规则并返回完整视图。
	writeStyle(&builder, bible.Style)
	return builder.String()
}

// RenderStatus 展示 HEAD 对应的当前世界，而不是重新让模型总结一次。
func RenderStatus(bible StoryBible, state State) string {
	// 状态页只展示 HEAD 对应快照的投影，帮助人工判断下一步，又不会引入新的事实。

	// 标明当前书名和提交章节，建立状态时间边界。
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 《%s》当前状态\n\n", bible.Title)
	fmt.Fprintf(&builder, "已提交到第 %d 章。\n\n", state.Chapter)
	fmt.Fprintf(&builder, "故事状态：%s；当前阶段：%s（%s）。\n\n", state.StoryStatus, state.OutlineProgress.CurrentMovementID, state.OutlineProgress.Status)

	// 输出人物动态、剧情线账本和最近章节记忆。
	writeCharacterStates(&builder, bible, state.Characters)
	writeThreads(&builder, state.Threads)
	writeRecentSummaries(&builder, state.Summaries)

	return builder.String()
}

// writeSection 输出一个带标题的 Markdown 区块。
func writeSection(builder *strings.Builder, title, content string) {
	// 统一章节格式，避免 story.md 的人读视图因字段不同而出现不一致排版。
	fmt.Fprintf(builder, "## %s\n\n%s\n\n", title, content)
}

// writeList 输出字符串列表，并为零项提供明确占位。
func writeList(builder *strings.Builder, title string, values []string) {
	// 空列表明确输出“暂无”，比省略整个区块更能让读者区分“没有内容”和“渲染遗漏”。
	// 输出区块标题。
	fmt.Fprintf(builder, "## %s\n\n", title)
	if len(values) == 0 {
		// 空集合使用明确占位，区分“暂无”与“渲染漏项”。
		builder.WriteString("暂无。\n\n")
		return
	}

	// 逐条输出列表内容。
	for _, value := range values {
		fmt.Fprintf(builder, "- %s\n", value)
	}
	builder.WriteString("\n")
}

// writeCharacters 输出静态人物底色信息。
func writeCharacters(builder *strings.Builder, characters []Character) {
	// 仅渲染静态人物底色，动态处境由 writeCharacterStates 单独展示，避免两类信息混在一起。
	// 输出区块标题和静态人物底色。
	builder.WriteString("## 主要人物\n\n")
	for _, character := range characters {
		fmt.Fprintf(builder, "### %s（%s）\n\n", character.Name, character.Role)
		fmt.Fprintf(builder, "- 底色：%s\n- 欲望：%s\n- 弱点：%s\n- 秘密：%s\n\n",
			character.Trait, character.Desire, character.Weakness, character.Secret)
	}
}

// writeStyle 输出全书共享的视角、基调和 prose 规则。
func writeStyle(builder *strings.Builder, style StyleGuide) {
	// 将视角/基调/规则集中放在文档末尾，便于写作者和人工审阅者快速查阅。
	// 输出视角与基调。
	builder.WriteString("## 叙事风格\n\n")
	fmt.Fprintf(builder, "- 视角：%s\n- 基调：%s\n", style.PointOfView, style.Tone)

	// 追加逐条可执行文风规则。
	for _, rule := range style.ProseRules {
		fmt.Fprintf(builder, "- %s\n", rule)
	}
	builder.WriteString("\n")
}

// writeCharacterStates 将动态人物状态中的 ID 投影为姓名后输出。
func writeCharacterStates(builder *strings.Builder, bible StoryBible, states []CharacterState) {
	// 先建立 ID 到姓名的投影，正文只显示人名；若遇到未知 ID，空名称也会暴露状态异常。
	// 建立人物 ID 到姓名的显示投影。
	nameByID := make(map[string]string, len(bible.Characters))
	for _, character := range bible.Characters {
		nameByID[character.ID] = character.Name
	}

	// 输出目标、情绪、位置和关系动态。
	builder.WriteString("## 人物现状\n\n")
	for _, state := range states {
		fmt.Fprintf(builder, "### %s\n\n", nameByID[state.CharacterID])
		fmt.Fprintf(builder, "- 目标：%s\n- 情绪：%s\n- 位置：%s\n", state.Goal, state.Emotion, state.Location)
		for _, relation := range state.Relationships {
			fmt.Fprintf(builder, "- 对 %s：%s（信任 %d，张力 %d）\n",
				nameByID[relation.TargetID], relation.Note, relation.Trust, relation.Tension)
		}
		builder.WriteString("\n")
	}
}

// writeThreads 输出剧情线账本及其推进/回收信息。
func writeThreads(builder *strings.Builder, threads []PlotThreadState) {
	// 账本同时展示类型、生命周期、最近推进和计划回收，帮助人判断节奏而非只看一句摘要。
	// 输出所有剧情线的生命周期和推进账本。
	builder.WriteString("## 剧情线账本\n\n")
	for _, thread := range threads {
		fmt.Fprintf(builder, "- **%s** [%s/%s]：%s（最近推进：第 %d 章）\n",
			thread.Name, thread.Kind, thread.Status, thread.Progress,
			thread.LastTouchedChapter)
	}
	builder.WriteString("\n")
}

// writeRecentSummaries 只输出最近三章摘要，控制状态页长度。
func writeRecentSummaries(builder *strings.Builder, summaries []ChapterSummary) {
	// 状态页只保留最近三章，控制可读长度；完整历史仍在 checkpoint 和章节文件中。
	// 计算最近三章的窗口。
	builder.WriteString("## 最近章节\n\n")
	start := max(0, len(summaries)-3)

	// 按原提交顺序输出摘要。
	for _, summary := range summaries[start:] {
		fmt.Fprintf(builder, "- 第 %d 章《%s》：%s\n", summary.Number, summary.Title, summary.Summary)
	}
	builder.WriteString("\n")
}
