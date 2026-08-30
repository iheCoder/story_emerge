package story

import (
	"fmt"
	"strings"
)

// RenderStory 把机器可校验的故事圣经投影成读者友好的 Markdown。
func RenderStory(bible StoryBible) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s\n\n", bible.Title)
	fmt.Fprintf(&builder, "> %s\n\n", bible.Logline)
	writeSection(&builder, "类型", bible.Genre)
	writeSection(&builder, "读者承诺", bible.ReaderPromise)
	writeSection(&builder, "最终方向", bible.Ending)
	writeArcs(&builder, bible.Arcs)
	writeCharacters(&builder, bible.Characters)
	writeList(&builder, "世界与正典规则", bible.CanonRules)
	writeList(&builder, "反复意象", bible.RecurringMotifs)
	writeStyle(&builder, bible.Style)
	return builder.String()
}

// RenderStatus 展示 HEAD 对应的当前世界，而不是重新让模型总结一次。
func RenderStatus(bible StoryBible, state State) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 《%s》当前状态\n\n", bible.Title)
	fmt.Fprintf(&builder, "已提交到第 %d 章。\n\n", state.Chapter)
	writeCharacterStates(&builder, bible, state.Characters)
	writeThreads(&builder, state.Threads)
	writeRecentSummaries(&builder, state.Summaries)
	writeList(&builder, "总导演下一阶段提示", state.DirectorGuidance)
	return builder.String()
}

func writeSection(builder *strings.Builder, title, content string) {
	fmt.Fprintf(builder, "## %s\n\n%s\n\n", title, content)
}

func writeList(builder *strings.Builder, title string, values []string) {
	fmt.Fprintf(builder, "## %s\n\n", title)
	if len(values) == 0 {
		builder.WriteString("暂无。\n\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "- %s\n", value)
	}
	builder.WriteString("\n")
}

func writeArcs(builder *strings.Builder, arcs []StoryArc) {
	builder.WriteString("## 故事阶段\n\n")
	for _, arc := range arcs {
		fmt.Fprintf(builder, "### %s（第 %d～%d 章）\n\n", arc.Name, arc.StartChapter, arc.EndChapter)
		fmt.Fprintf(builder, "目标：%s\n\n高潮：%s\n\n", arc.Goal, arc.Climax)
	}
}

func writeCharacters(builder *strings.Builder, characters []Character) {
	builder.WriteString("## 主要人物\n\n")
	for _, character := range characters {
		fmt.Fprintf(builder, "### %s（%s）\n\n", character.Name, character.Role)
		fmt.Fprintf(builder, "- 底色：%s\n- 欲望：%s\n- 弱点：%s\n- 秘密：%s\n\n",
			character.Trait, character.Desire, character.Weakness, character.Secret)
	}
}

func writeStyle(builder *strings.Builder, style StyleGuide) {
	builder.WriteString("## 叙事风格\n\n")
	fmt.Fprintf(builder, "- 视角：%s\n- 基调：%s\n", style.PointOfView, style.Tone)
	for _, rule := range style.ProseRules {
		fmt.Fprintf(builder, "- %s\n", rule)
	}
	builder.WriteString("\n")
}

func writeCharacterStates(builder *strings.Builder, bible StoryBible, states []CharacterState) {
	nameByID := make(map[string]string, len(bible.Characters))
	for _, character := range bible.Characters {
		nameByID[character.ID] = character.Name
	}
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

func writeThreads(builder *strings.Builder, threads []PlotThreadState) {
	builder.WriteString("## 剧情线账本\n\n")
	for _, thread := range threads {
		fmt.Fprintf(builder, "- **%s** [%s/%s]：%s（最近推进：第 %d 章，计划回收：第 %d 章）\n",
			thread.Name, thread.Kind, thread.Status, thread.Progress,
			thread.LastTouchedChapter, thread.PlannedPayoffChapter)
	}
	builder.WriteString("\n")
}

func writeRecentSummaries(builder *strings.Builder, summaries []ChapterSummary) {
	builder.WriteString("## 最近章节\n\n")
	start := max(0, len(summaries)-3)
	for _, summary := range summaries[start:] {
		fmt.Fprintf(builder, "- 第 %d 章《%s》：%s\n", summary.Number, summary.Title, summary.Summary)
	}
	builder.WriteString("\n")
}
