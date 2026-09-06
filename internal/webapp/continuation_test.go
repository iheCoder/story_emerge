package webapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/workflow"
)

// controlledGrowthRuntime 用通道暂停一次续写，使测试能稳定观察请求已经接受但尚未完成的窗口。
// 不调用真实模型；真实文件仓库负责验证重启发现与正式章节读取。
type controlledGrowthRuntime struct {
	calls   chan int
	release chan error
}

func (*controlledGrowthRuntime) CreatePreview(context.Context, string, CreateRequest, workflow.Reporter) error {
	return errors.New("此测试只验证已初始化项目的继续生长")
}
func (runtime *controlledGrowthRuntime) Continue(ctx context.Context, _ string, limit int, _ workflow.Reporter) error {
	runtime.calls <- limit
	select {
	case err := <-runtime.release:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// prepareGrowthBook 写入可被全新服务器发现的正式历史，章数为 0 时表示初始化成功但还没有正文。
func prepareGrowthBook(t *testing.T, root string, chapters int, complete bool) {
	t.Helper()
	files := store.New(filepath.Join(root, "book"))
	if err := files.Prepare(story.Project{Idea: "两人共同生活", LengthProfile: "medium", MaxCalls: 100}); err != nil {
		t.Fatal(err)
	}
	core := story.StoryCore{
		StoryEngine:        story.StoryEngine{Loop: "回应带来后果", ProgressionAxis: "共同承担"},
		ReaderPromises:     []story.ReaderPromise{{Promise: "生活", PayoffShape: "日常"}, {Promise: "信任", PayoffShape: "选择"}, {Promise: "责任", PayoffShape: "结果"}},
		ExperienceContract: story.ExperienceContract{TargetExperience: "平凡生活的温暖"},
	}
	if err := files.CommitGenesis(story.Genesis{Title: "生长测试", StoryCore: core, StorySpine: []string{"临时相处逐渐成为能在重要决定中相互依靠的关系"}, CurrentDirection: story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "共同生活", DesiredShift: "信任", ReaderExpectation: "读者等待信任如何形成"}}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= chapters; n++ {
		commit := story.ChapterCommit{
			Chapter: n, Title: fmt.Sprintf("第%d章 晚饭", n),
			Review: story.EditorDecision{ChapterDecision: story.EditorAccept, Assessment: story.EditorAssessment{Contribution: "感受信任", Sequence: "继续积累", Execution: "正文成立"}, StoryComplete: complete && n == chapters},
			Result: story.CommitResult{ChapterSummary: "两人共享晚饭", TrajectoryEntry: story.TrajectoryMove{StoryMove: "通过日常感受信任", NarrativeShape: "做饭 → 相处"}},
		}
		if _, err := files.CommitChapter(fmt.Sprintf("# 第%d章 晚饭\n\n两人坐下来吃饭。", n), commit); err != nil {
			t.Fatal(err)
		}
	}
}

// growthRequest 通过实际 HTTP 路由调用接口，而非绕过参数和并发检查直接调用运行器。
func growthRequest(server *Server, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

// waitGrowthStopped 等待异步收尾写完展示状态，避免读取状态与测试清理目录之间发生竞争。
func waitGrowthStopped(t *testing.T, server *Server) job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := server.copyJob("book")
		if !current.Running {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("续写任务没有结束")
	return job{}
}

func TestContinueGrowthBeforePreviewCompletionAndAfterRestart(t *testing.T) {
	// 场景：初始化完成后的 0/1/2/3 章项目由新服务器从磁盘恢复，没有内存中的 failed 标记。
	// 预期：刷新能够读取项目，逐章入口均可启动且只请求一章；全书入口在三章前仍不开放。
	for _, chapters := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprint(chapters), func(t *testing.T) {
			root := t.TempDir()
			prepareGrowthBook(t, root, chapters, false)
			runtime := &controlledGrowthRuntime{calls: make(chan int, 4), release: make(chan error, 4)}
			server, err := NewServer(context.Background(), root, runtime)
			if err != nil {
				t.Fatal(err)
			}

			// 相当于浏览器刷新后重新 GET；恢复后的 ready 不应使前三章的逐章入口被禁止。
			if response := growthRequest(server, http.MethodGet, "/api/stories/book"); response.Code != http.StatusOK {
				t.Fatal(response.Body)
			}
			if chapters < 3 {
				if response := growthRequest(server, http.MethodPost, "/api/stories/book/complete"); response.Code != http.StatusConflict {
					t.Fatal("试读前意外开放全书生成")
				}
			}
			response := growthRequest(server, http.MethodPost, "/api/stories/book/next")
			if response.Code != http.StatusAccepted {
				t.Fatalf("继续生长被拒绝: %s", response.Body)
			}
			select {
			case limit := <-runtime.calls:
				if limit != 1 {
					t.Fatalf("逐章生长请求了 %d 章", limit)
				}
			case <-time.After(time.Second):
				t.Fatal("请求没有传递给运行器")
			}

			// 运行器阻塞期间模拟另一页面重复点击。只能有一个任务，不因按钮已显示而允许并发续写。
			duplicate := growthRequest(server, http.MethodPost, "/api/stories/book/next")
			runtime.release <- errors.New("模拟第三章生成失败")
			failed := waitGrowthStopped(t, server)
			if duplicate.Code != http.StatusConflict {
				t.Fatalf("重复请求未拒绝: %s", duplicate.Body)
			}
			if failed.Status != "failed" || failed.Running {
				t.Fatalf("失败没有释放任务: %#v", failed)
			}

			// 故障收尾后再次点击必须重新开放，并清掉旧错误；重试仍是逐章生成。
			// 提前提供结果使后台任务立即完成，避免测试等待人为输入。
			runtime.release <- nil
			response = growthRequest(server, http.MethodPost, "/api/stories/book/next")
			if response.Code != http.StatusAccepted {
				t.Fatalf("失败后不能继续: %s", response.Body)
			}
			ready := waitGrowthStopped(t, server)
			if ready.Status != "ready" || ready.Error != "" {
				t.Fatalf("旧错误未清除: %#v", ready)
			}
		})
	}
}

func TestCompletedStoryRejectsGrowthEvenBeforeThreeChapters(t *testing.T) {
	// 场景：Editor 在第一章确认完结。短于试读章数不意味着用户可以继续生成。
	// 预期：逐章与全书接口都按正式完结状态拒绝，完全不进入运行器。
	root := t.TempDir()
	prepareGrowthBook(t, root, 1, true)
	runtime := &controlledGrowthRuntime{calls: make(chan int, 2), release: make(chan error, 2)}
	server, err := NewServer(context.Background(), root, runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"next", "complete"} {
		response := growthRequest(server, http.MethodPost, "/api/stories/book/"+action)
		if response.Code != http.StatusConflict {
			t.Fatalf("已完结仍能生成: %s", response.Body)
		}
	}
	if len(runtime.calls) != 0 {
		t.Fatal("完结后调用了运行器")
	}
}
