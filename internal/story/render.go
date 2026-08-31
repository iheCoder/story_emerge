package story

import (
	"fmt"
	"strings"
)

// RenderStory 把 Story Bible 投影成便于人工检查的 Markdown。
func RenderStory(bible StoryBible) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s\n\n", bible.Title)
	writeSection(&builder, "故事前提", bible.Premise)
	writeSection(&builder, "Story Spine", bible.StorySpine)
	writeSection(&builder, "目标读者", bible.TargetReader.Portrait)
	writeSection(&builder, "阅读动机", bible.TargetReader.ReadsFor)
	writeSection(&builder, "核心阅读体验", bible.NarrativePromise.CoreExperience)
	writeSection(&builder, "最终方向", bible.EndingDirection)

	writeCharacters(&builder, bible.Characters)
	writeList(&builder, "世界规则", bible.WorldRules)
	writeList(&builder, "稳定事实", bible.StableFacts)
	writeStyle(&builder, bible.Style)

	return builder.String()
}

// RenderStatus 只展示 HEAD 的长期当前态，不把短期章节流水重新塞回状态页。
func RenderStatus(bible StoryBible, state State) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 《%s》当前状态\n\n", bible.Title)
	fmt.Fprintf(&builder, "已提交到第 %d 章；故事状态：%s。\n\n", state.Chapter, state.StoryStatus)

	writeCharacterStates(&builder, bible, state.CharacterStates)
	writeSituationStates(&builder, state.SituationStates)
	writeList(&builder, "当前叙事张力", state.LiveTensions)

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
	names := make(map[string]string, len(bible.Characters))
	for _, character := range bible.Characters {
		names[character.ID] = character.Name
	}

	builder.WriteString("## 人物长期现状\n\n")
	for _, state := range states {
		fmt.Fprintf(builder, "- **%s**：%s\n", names[state.CharacterID], state.State)
	}
	builder.WriteString("\n")
}

func writeSituationStates(builder *strings.Builder, states []SituationState) {
	builder.WriteString("## 当前客观局势\n\n")
	for _, state := range states {
		fmt.Fprintf(builder, "- **%s**：%s\n", state.ID, state.Description)
	}
	builder.WriteString("\n")
}
