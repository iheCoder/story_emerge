package workflow

import (
	"reflect"
	"testing"

	"story_emerge/internal/story"
)

func TestSingleParticipantRelationshipCanBeSaved(t *testing.T) {
	// 场景：主角开始信任尚未单独建档的保安，关系中只索引已建档的主角。
	// 预期：初始化和 Commit 均接受该结构，保存时不补造人物，也不丢弃关系。
	relation := story.RelationshipState{ID: "r9", Characters: []string{"a"}, Description: "主角开始信任经常替他留门的夜班保安"}
	genesis := testGenesis()
	genesis.InitialStoryState.Relationships = []story.RelationshipState{relation}
	commit := extracted()
	commit.StatePatch.Relationships.Upsert = []story.RelationshipState{relation}

	for _, tc := range []struct {
		template, schema string
		value            any
	}{
		{"architect", "genesis", genesis}, {"commit", "chapter_commit", commit},
	} {
		t.Run(tc.template, func(t *testing.T) {
			// 使用实际嵌入的 Schema，而非在测试里另写规则，防止本地校验和模型看到的契约脱节。
			_, schema, err := loadPromptAndSchema(tc.template, tc.schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateOutputShape(mustJSON(tc.value), schema); err != nil {
				t.Fatalf("单个参与者被 Schema 拒绝: %v", err)
			}
		})
	}

	// 从无关系的状态应用补丁，确认领域校验接受新增关系并完整保留参与者索引。
	initial, err := story.ResolveInitialStateItemIDs(genesis.InitialStoryState)
	if err != nil {
		t.Fatalf("初始人物状态 ID 生成失败: %v", err)
	}
	genesis.InitialStoryState = initial
	if err := story.ValidateStoryState(genesis.InitialStoryState); err != nil {
		t.Fatalf("初始化状态被拒绝: %v", err)
	}
	before, err := story.ResolveInitialStateItemIDs(testGenesis().InitialStoryState)
	if err != nil {
		t.Fatal(err)
	}
	next, err := story.ApplyPatch(before, commit.StatePatch)
	if err != nil {
		t.Fatalf("单个参与者的关系无法提交: %v", err)
	}
	if !reflect.DeepEqual(next.Relationships, []story.RelationshipState{relation}) || !reflect.DeepEqual(next.Characters, before.Characters) {
		t.Fatalf("保存关系时丢失数据或额外创建了人物: %#v", next)
	}
}
