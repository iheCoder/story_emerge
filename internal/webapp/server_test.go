package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/workflow"
)

const exactInspiration = `为改变暗恋对象死亡的命运，顾以谕以“供名身份"穿越回过去，重写结局。但只要抵达特定的时间节点，他的存在会被抹去，如同从未出现在余然宸的生命中。
顾以谕不在乎被忘记，能救喜欢的人就好。
结果某次穿越，他被余然宸偷亲了一口。
罪魁祸首亲完就睡着，呼吸绵长。
只留顾以谕不敢动，大脑死机，一整宿都想不通问题出在哪里。`

type fakeRuntime struct {
	mu       sync.Mutex
	requests []CreateRequest
}

func (runtime *fakeRuntime) CreatePreview(
	_ context.Context,
	root string,
	request CreateRequest,
	reporter workflow.Reporter,
) error {
	runtime.mu.Lock()
	runtime.requests = append(runtime.requests, request)
	runtime.mu.Unlock()

	files := store.New(root)
	project, genesis := webFixture(request)
	if err := files.Create(project, genesis); err != nil {
		return err
	}
	return commitFixtureChapters(files, project.TargetChapters, 3, reporter)
}

func (runtime *fakeRuntime) Continue(
	_ context.Context,
	root string,
	limit int,
	reporter workflow.Reporter,
) error {
	files := store.New(root)
	project, err := files.LoadProject()
	if err != nil {
		return err
	}
	state, err := files.LoadState()
	if err != nil {
		return err
	}
	remaining := project.TargetChapters - state.Chapter
	if limit > 0 && limit < remaining {
		remaining = limit
	}
	return commitFixtureChapters(files, project.TargetChapters, remaining, reporter)
}

func TestWebFlowCreatesPreviewReadsChapterAndGeneratesNext(t *testing.T) {
	// 场景：使用用户提供的完整灵感创建中篇，等待前三章，再阅读第一章并生成第四章。
	// 预期：API 保留原始灵感、前三章逐章可读，next 只推进一个提交章节。
	runtime := &fakeRuntime{}
	application, err := NewServer(context.Background(), filepath.Join(t.TempDir(), "novels"), runtime)
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()

	id := createStoryForTest(t, handler, exactInspiration, "medium")
	preview := waitForChapter(t, handler, id, 3)
	if preview.Title != "被忘记以前" || len(preview.Chapters) != 3 {
		t.Fatalf("三章试读投影错误: %#v", preview)
	}
	assertExactRequest(t, runtime)

	chapter := getChapterForTest(t, handler, id, 1)
	if chapter.Title != "供名者" || !strings.Contains(chapter.Body, "顾以谕") {
		t.Fatalf("第一章内容错误: %#v", chapter)
	}

	postForTest(t, handler, "/api/stories/"+id+"/next", nil, http.StatusAccepted)
	continued := waitForChapter(t, handler, id, 4)
	if continued.CurrentChapter != 4 || len(continued.Chapters) != 4 {
		t.Fatalf("next 应只生成第四章: %#v", continued)
	}
}

func TestWebHomeIsTheCreationExperience(t *testing.T) {
	// 场景：用户直接打开 Web 根路径。
	// 预期：第一屏就是故事灵感与篇幅输入，而不是营销落地页或 Dashboard。
	application, err := NewServer(context.Background(), t.TempDir(), &fakeRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "你想进入一个") {
		t.Fatalf("首页没有渲染真实创作入口: code=%d", response.Code)
	}
}

func TestLibraryRestoresCommittedStoryAfterServerRestart(t *testing.T) {
	// 场景：前三章已经提交，Web 服务随后重启，同时书架目录里还混有无效文件夹。
	// 预期：新进程只恢复有效小说，保留 Agent 生成的书名，并且仍可读取和续写第四章。
	root := filepath.Join(t.TempDir(), "novels")
	first, err := NewServer(context.Background(), root, &fakeRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	id := createStoryForTest(t, first.Handler(), exactInspiration, "medium")
	waitForChapter(t, first.Handler(), id, 3)
	if err := os.MkdirAll(filepath.Join(root, "unfinished-junk"), 0o755); err != nil {
		t.Fatal(err)
	}

	secondRuntime := &fakeRuntime{}
	second, err := NewServer(context.Background(), root, secondRuntime)
	if err != nil {
		t.Fatal(err)
	}
	handler := second.Handler()
	library := getLibraryForTest(t, handler)
	if len(library) != 1 || library[0].ID != id || library[0].Title != "被忘记以前" {
		t.Fatalf("书架恢复结果错误: %#v", library)
	}
	if chapter := getChapterForTest(t, handler, id, 2); chapter.Title != "偷来的雨夜" {
		t.Fatalf("重启后章节不可读: %#v", chapter)
	}

	postForTest(t, handler, "/api/stories/"+id+"/next", nil, http.StatusAccepted)
	continued := waitForChapter(t, handler, id, 4)
	if continued.CurrentChapter != 4 {
		t.Fatalf("重启后未能继续生成: %#v", continued)
	}
}

func webFixture(request CreateRequest) (story.Project, story.Genesis) {
	project := story.Project{
		Version: story.FormatVersion, Name: "demo", Idea: request.Idea, LengthProfile: request.Length,
		Provider: "fake", Model: "fake", TargetChapters: 4,
		ChapterMinChars: 500, ChapterMaxChars: 800, MaxCalls: 20, CreatedAt: time.Unix(0, 0).UTC(),
	}
	genesis := story.Genesis{
		TargetChapters: 4,
		Bible: story.StoryBible{
			Title: "被忘记以前", Logline: "顾以谕一次次被抹去，却仍想改写余然宸的结局。",
			Characters: []story.Character{{ID: "hero", Name: "顾以谕"}},
		},
		InitialState: story.InitialState{Characters: []story.CharacterState{{CharacterID: "hero"}}},
	}
	return project, genesis
}

func commitFixtureChapters(files *store.Store, target, count int, reporter workflow.Reporter) error {
	state, err := files.LoadState()
	if err != nil {
		return err
	}
	for index := 0; index < count && state.Chapter < target; index++ {
		number := state.Chapter + 1
		title := []string{"供名者", "偷来的雨夜", "遗忘发生以前", "被记住的意外"}[number-1]
		reporter(workflow.Event{Stage: "chapter", Message: fmt.Sprintf("开始第 %d 章", number)})
		state.Chapter = number
		state.Summaries = append(state.Summaries, story.ChapterSummary{Number: number, Title: title, Summary: "命运继续向前。"})
		delta := story.StateDelta{Chapter: number, Summary: state.Summaries[len(state.Summaries)-1]}
		chapter := fmt.Sprintf("# 第%d章 %s\n\n顾以谕在这一章再次走向余然宸。\n", number, title)
		if err := files.CommitChapter(story.ChapterPlan{Number: number, Title: title}, chapter, delta, story.Review{Passed: true}, state); err != nil {
			return err
		}
		reporter(workflow.Event{Stage: "chapter", Message: fmt.Sprintf("第 %d 章已提交，评分 88", number)})
	}
	return nil
}

func createStoryForTest(t *testing.T, handler http.Handler, idea, length string) string {
	t.Helper()
	response := postForTest(t, handler, "/api/stories", CreateRequest{Idea: idea, Length: length}, http.StatusAccepted)
	var payload map[string]string
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatal(err)
	}
	return payload["id"]
}

func waitForChapter(t *testing.T, handler http.Handler, id string, chapter int) storySnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request := httptest.NewRequest(http.MethodGet, "/api/stories/"+id, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var snapshot storySnapshot
		decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
		if decodeErr == nil && snapshot.CurrentChapter >= chapter {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待第 %d 章超时", chapter)
	return storySnapshot{}
}

func getChapterForTest(t *testing.T, handler http.Handler, id string, number int) chapterResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/stories/%s/chapters/%d", id, number), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var chapter chapterResponse
	if err := json.NewDecoder(response.Body).Decode(&chapter); err != nil {
		t.Fatal(err)
	}
	return chapter
}

func getLibraryForTest(t *testing.T, handler http.Handler) []storySnapshot {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/stories", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/stories 状态错误: %d %s", response.Code, response.Body.String())
	}
	var library []storySnapshot
	if err := json.NewDecoder(response.Body).Decode(&library); err != nil {
		t.Fatal(err)
	}
	return library
}

func postForTest(t *testing.T, handler http.Handler, path string, value any, expectedStatus int) []byte {
	t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(http.MethodPost, path, body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	content, _ := io.ReadAll(response.Body)
	if response.Code != expectedStatus {
		t.Fatalf("POST %s 状态错误: %d %s", path, response.Code, content)
	}
	return content
}

func assertExactRequest(t *testing.T, runtime *fakeRuntime) {
	t.Helper()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.requests) != 1 || runtime.requests[0].Idea != exactInspiration || runtime.requests[0].Length != "medium" {
		t.Fatalf("Web 输入没有原样进入工作流: %#v", runtime.requests)
	}
}
