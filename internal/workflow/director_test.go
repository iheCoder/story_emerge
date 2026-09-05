package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
)

func TestStoryDirectorCanRunAloneWithoutTouchingProductionState(t *testing.T) {
	// 场景：绕过生产触发器，独立执行一次 Story Director，模拟人工审计。
	// 预期：底层调用只生成并校验决定；没有正式提交调用时，结果只落在 .work，不创建 HEAD 或 checkpoint。
	root := filepath.Join(t.TempDir(), "novel")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	files := store.New(root)
	current := story.Direction{
		Focus:             "两人的合作从方便逐渐变成一种生活选择",
		DesiredShift:      "双方开始主动为共同生活承担代价",
		ReaderExpectation: "这种主动选择遇到外部压力时能否继续成立",
	}
	decision := story.DirectorDecision{Action: story.DirectorKeep, Direction: current, Reason: "近期变化仍在累积，尚未形成阶段性结果"}
	fake := &scriptedGenerator{
		responses: map[string]string{"chapter_004_director": mustJSON(decision)},
		failures:  map[string]error{},
		requests:  map[string]llm.Request{},
	}
	engine, err := New(fake, files, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := directorInput{
		StoryCore:         testGenesis().StoryCore,
		CurrentStoryState: testGenesis().InitialStoryState,
		CurrentDirection:  current,
		RecentTrajectory:  []story.TrajectoryEntry{{Chapter: 3, TrajectoryMove: story.TrajectoryMove{StoryMove: "共同承担了一次损失", NarrativeShape: "试探 → 主动留下"}}},
		ChapterLedger:     []story.LedgerEntry{{Chapter: 3, Title: "留下", Summary: "双方第一次主动承担共同后果"}},
		StoryProgress:     lengthProgress{TargetLength: "约 8～10 万字", WrittenCharacters: 12000},
		EditorEscalation:  "连续两章的关系作用相似，需要判断方向是否停滞",
	}

	got, err := engine.reviewStoryDirection(context.Background(), 4, input)
	if err != nil {
		t.Fatal(err)
	}
	if got != decision {
		t.Fatalf("Director 决定解析错误: got=%#v want=%#v", got, decision)
	}

	// 检查真实发给模型的请求，防止仅凭 Go 类型推断角色隔离已经成立。
	request := fake.requests["chapter_004_director"]
	if request.Role != llm.RoleDirector || request.SchemaName != "director_decision" {
		t.Fatalf("Director 路由或 Schema 错误: %#v", request)
	}
	for _, required := range []string{"story_core", "current_story_state", "current_direction", "recent_trajectory", "chapter_ledger", "story_progress", "editor_escalation"} {
		if !strings.Contains(request.Input, required) {
			t.Fatalf("Director 输入缺少阶段依据: %s", required)
		}
	}
	for _, forbidden := range []string{"user_idea", "chapter_intent", "previous_chapter", "next_chapter", "story_status"} {
		if strings.Contains(strings.ToLower(request.Input), forbidden) || strings.Contains(strings.ToLower(request.Instructions), forbidden) {
			t.Fatalf("Director 请求混入越权字段: %s", forbidden)
		}
	}

	// 独立组件允许保存可审计结果，但不创建 HEAD、checkpoint、commit 或章节正文。
	if _, err := os.Stat(filepath.Join(root, ".work", "004-chapter_004_director.json")); err != nil {
		t.Fatalf("Director 决定没有进入诊断目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "HEAD")); !os.IsNotExist(err) {
		t.Fatalf("独立 Director 意外创建或改写 HEAD: %v", err)
	}
}

func TestDirectorStructuredOutputRejectsStoryStatus(t *testing.T) {
	// 场景：模型沿用旧设想，在合法决定之外额外返回 story_status。
	// 预期：严格 JSON 解码直接拒绝未知字段，完结权不会从 Editor 漂移到 Director。
	output := `{"action":"KEEP","direction":{"focus":"重心","desired_shift":"变化","reader_expectation":"期待"},"reason":"继续积累","story_status":"ONGOING"}`
	if _, err := decodeStructured[story.DirectorDecision](output); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("story_status 未被严格拒绝: %v", err)
	}
}
