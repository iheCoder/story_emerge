package story

import (
	"reflect"
	"testing"
)

func TestInitialWorldIDsAreStableAndRemainPatchable(t *testing.T) {
	// 场景：Architect 给世界事实留空 ID；已有 ID 与人物/关系身份必须保留。
	// 预期：初始化补标识但不改内容，重试、重排和再次归一化均稳定；后续仍按原 ID 更新。
	raw := CurrentStoryState{World: []WorldFact{
		{Description: "小区门口的日常物品陆续消失。"},
		{ID: "existing", Description: "尚未发生人员受伤。"},
		{Description: "居民通过业主群互通信息。"},
	}}
	before := cloneStoryState(raw)
	first, err := ResolveInitialStateItemIDs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if first.World[0].ID == "" || first.World[2].ID == "" || first.World[0].ID == first.World[2].ID || first.World[1] != raw.World[1] {
		t.Fatalf("世界事实标识没有正确建立: %#v", first.World)
	}
	for _, input := range []CurrentStoryState{raw, first} {
		again, err := ResolveInitialStateItemIDs(input)
		if err != nil || !reflect.DeepEqual(again, first) {
			t.Fatalf("初始化标识不稳定: %#v %v", again, err)
		}
	}
	reordered := cloneStoryState(raw)
	reordered.World[0], reordered.World[2] = reordered.World[2], reordered.World[0]
	again, err := ResolveInitialStateItemIDs(reordered)
	if err != nil || again.World[2] != first.World[0] || again.World[0] != first.World[2] {
		t.Fatalf("重排改变了标识: %#v %v", again, err)
	}
	if !reflect.DeepEqual(cloneStoryState(raw), before) {
		t.Fatal("归一化污染原始输出")
	}
	for i := range raw.World {
		if first.World[i].Description != raw.World[i].Description {
			t.Fatal("补 ID 改写了故事事实")
		}
	}

	// 内容更新仍引用首次分配的 ID，不因描述变化而再分配身份；遗漏条目保持原样。
	update := WorldFact{ID: first.World[0].ID, Description: "警方已找回部分失物。"}
	next, err := ApplyPatch(first, StatePatch{World: CollectionPatch[WorldFact]{Upsert: []WorldFact{update}}})
	if err != nil || next.World[0] != update || !reflect.DeepEqual(next.World[1:], first.World[1:]) {
		t.Fatalf("生成标识无法精确更新: %#v %v", next, err)
	}
}

func TestInitialWorldIDResolutionStillRejectsInvalidFacts(t *testing.T) {
	// 场景：空内容、重复已有 ID、重复无 ID 内容均不能用自动补 ID 掩盖。
	// 预期：失败不修改原始状态，也不静默删除或合并事实。
	for _, world := range [][]WorldFact{
		{{Description: " "}},
		{{ID: "same", Description: "甲"}, {ID: "same", Description: "乙"}},
		{{Description: "甲"}, {Description: "甲"}},
	} {
		raw := CurrentStoryState{World: world}
		before := cloneStoryState(raw)
		if _, err := ResolveInitialStateItemIDs(raw); err == nil {
			t.Fatalf("无效事实被接受: %#v", world)
		}
		if !reflect.DeepEqual(cloneStoryState(raw), before) {
			t.Fatal("失败污染了输入")
		}
	}
}
