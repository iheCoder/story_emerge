package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

func TestInitializationPreservesRawWorldIDsAndCommitsResolvedState(t *testing.T) {
	// 场景：Architect 输出内容完整的世界事实但 ID 留空，与现场故障结构一致。
	// 预期：正常初始化只调用一次模型；原始候选留空，正式 checkpoint 和后续 Writer 得到相同的稳定 ID。
	candidate := testGenesis()
	candidate.InitialStoryState.World = []story.WorldFact{{Description: "门口的日常物品陆续消失。"}, {Description: "居民通过业主群互通信息。"}}
	fake := newFake(1)
	fake.responses["architect"] = mustJSON(candidate)
	engine, files := initializeTest(t, fake)
	data, err := os.ReadFile(filepath.Join(files.Root(), ".work", "000-genesis.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw story.Genesis
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw.InitialStoryState.World, candidate.InitialStoryState.World) {
		t.Fatal("原始输出被归一化覆盖")
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 0 || state.Story.World[0].ID == "" {
		t.Fatalf("初始 checkpoint 无效: %#v %v", state, err)
	}

	// 绕过 workflow 的提交也必须归一化，验证两个入口产生相同的正式状态。
	direct := store.New(filepath.Join(t.TempDir(), "direct"))
	if err := direct.Prepare(testProject()); err != nil {
		t.Fatal(err)
	}
	if err := direct.CommitGenesis(candidate); err != nil {
		t.Fatal(err)
	}
	directState, err := direct.LoadState()
	if err != nil || !reflect.DeepEqual(directState, state) {
		t.Fatalf("直接提交与 workflow 结果不同: %#v %v", directState, err)
	}
	if err := engine.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var input struct {
		CurrentStoryState story.CurrentStoryState `json:"current_story_state"`
	}
	if err := json.Unmarshal([]byte(fake.requests["chapter_001_write"].Input), &input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.CurrentStoryState.World, state.Story.World) {
		t.Fatal("Writer 未读取正式世界事实 ID")
	}
	for stage := range fake.requests {
		if stage == "architect_format_repair" {
			t.Fatal("补技术 ID 不应增加模型调用")
		}
	}
}
