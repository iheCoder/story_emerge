// Package webapp 提供面向普通读者的轻量 Web 产品入口。
// 它只编排任务和投影已提交状态，不复制或绕过 workflow.Engine 的小说业务。
package webapp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/workflow"
)

const maxRequestBytes = 64 << 10

//go:embed static/*
var embeddedAssets embed.FS

// Server 管理 HTTP 路由与当前进程内的异步任务状态。
// 小说事实仍在每个项目的 HEAD/checkpoint 中，job 只保存展示阶段和错误信息。
type Server struct {
	ctx     context.Context
	root    string
	runtime Runtime
	nextID  atomic.Uint64
	mu      sync.RWMutex
	jobs    map[string]*job
}

type job struct {
	ID        string
	Root      string
	Length    string
	Status    string
	Phase     string
	Error     string
	Running   bool
	UpdatedAt time.Time
	Events    []progressEvent
}

type progressEvent struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// NewServer 创建可嵌入 http.Server 的处理器，不隐式启动监听端口。
func NewServer(ctx context.Context, root string, runtime Runtime) (*Server, error) {
	if runtime == nil {
		return nil, fmt.Errorf("web runtime 不能为空")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	server := &Server{ctx: ctx, root: absolute, runtime: runtime, jobs: make(map[string]*job)}
	if err := server.syncLibrary(); err != nil {
		return nil, err
	}
	return server, nil
}

// Handler 注册静态资源和全部故事 API。
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/stories", server.handleCreate)
	mux.HandleFunc("GET /api/stories", server.handleLibrary)
	mux.HandleFunc("GET /api/stories/{id}", server.handleStory)
	mux.HandleFunc("GET /api/stories/{id}/chapters/{number}", server.handleChapter)
	mux.HandleFunc("POST /api/stories/{id}/next", server.handleNext)
	mux.HandleFunc("POST /api/stories/{id}/complete", server.handleComplete)
	mux.Handle("GET /", staticHandler())
	return mux
}

func staticHandler() http.Handler {
	assets, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// 本地创作产品会频繁迭代嵌入资源；禁止陈旧缓存，避免服务重启后仍显示旧交互。
		response.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(response, request)
	})
}

// handleCreate 校验两个产品输入并立即返回任务 ID，真实模型调用在后台执行。
func (server *Server) handleCreate(response http.ResponseWriter, request *http.Request) {
	var input CreateRequest
	if err := decodeRequest(response, request, &input); err != nil {
		return
	}
	input.Length = strings.ToLower(strings.TrimSpace(input.Length))
	if err := validateCreateRequest(input); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}

	created := server.createJob(input.Length)
	writeJSON(response, http.StatusAccepted, map[string]string{"id": created.ID})
	go server.run(created.ID, "creating", func(reporter workflow.Reporter) error {
		return server.runtime.CreatePreview(server.ctx, created.Root, input, reporter)
	})
}

func validateCreateRequest(request CreateRequest) error {
	if strings.TrimSpace(request.Idea) == "" {
		return fmt.Errorf("请先写下你的故事灵感")
	}
	if len([]rune(request.Idea)) > 10000 {
		return fmt.Errorf("故事灵感不能超过 10000 个字符")
	}
	if !map[string]bool{"short": true, "medium": true, "long": true, "epic": true}[request.Length] {
		return fmt.Errorf("篇幅必须是 short、medium、long 或 epic")
	}
	return nil
}

func (server *Server) createJob(length string) *job {
	sequence := server.nextID.Add(1)
	id := "story-" + strconv.FormatInt(time.Now().UTC().UnixMilli(), 36) + "-" + strconv.FormatUint(sequence, 36)
	created := &job{
		ID: id, Root: filepath.Join(server.root, id), Length: length,
		Status: "creating", Phase: "正在理解你的故事", Running: true, UpdatedAt: time.Now().UTC(),
	}
	server.mu.Lock()
	server.jobs[id] = created
	server.mu.Unlock()
	return created
}

// run 统一收敛异步操作的成功与失败状态，避免 goroutine 分支漏掉 Running 清理。
func (server *Server) run(id, operation string, execute func(workflow.Reporter) error) {
	reporter := func(event workflow.Event) { server.recordEvent(id, event) }
	err := execute(reporter)
	// 展示状态以已提交正文为准；预览期间提前完结也应显示真实结局。
	snapshot, _ := server.copyJob(id)
	committed, stateErr := store.New(snapshot.Root).LoadState()
	if err == nil && stateErr != nil {
		err = stateErr
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	current := server.jobs[id]
	current.Running = false
	current.UpdatedAt = time.Now().UTC()
	if err != nil {
		current.Status = "failed"
		current.Phase = "故事暂时停在这里"
		current.Error = err.Error()
		return
	}
	current.Status = "ready"
	current.Phase = completionMessage(operation)
	if committed.Completed {
		current.Status = "complete"
		current.Phase = "故事已经完整收束"
	}
}

func completionMessage(operation string) string {
	switch operation {
	case "creating":
		return "三章试读已经准备好了"
	case "next":
		return "新的一章已经准备好了"
	default:
		return "故事已经完整收束"
	}
}

func (server *Server) recordEvent(id string, event workflow.Event) {
	server.mu.Lock()
	defer server.mu.Unlock()
	current := server.jobs[id]
	translated := progressEvent{Stage: event.Stage, Message: translateEvent(event)}
	current.Events = append(current.Events, translated)
	if len(current.Events) > 8 {
		current.Events = append([]progressEvent(nil), current.Events[len(current.Events)-8:]...)
	}
	current.Phase = translated.Message
	current.UpdatedAt = time.Now().UTC()
}

// translateEvent 把工程阶段翻译成读者可以感知的文学创作状态。
func translateEvent(event workflow.Event) string {
	stage := event.Stage
	switch {
	case stage == "architect" && strings.Contains(event.Message, "已建立"):
		return "人物与命运开始显影"
	case stage == "architect":
		return "正在理解你的故事"
	case stage == "chapter" && strings.Contains(event.Message, "已提交"):
		return strings.Replace(event.Message, "已提交", "已经准备好了", 1)
	case stage == "chapter":
		return event.Message + "正在形成"
	case strings.Contains(stage, "_plan_"):
		return chapterLabel(stage) + "正在重新寻找方向"
	case strings.Contains(stage, "_write_"):
		return chapterLabel(stage) + "正在写下发生的一切"
	case strings.Contains(stage, "_editor_"):
		return "正在确认这一章是否自然成立"
	case strings.HasSuffix(stage, "_commit"):
		return "记住这一章真正重要的长期变化"
	case strings.Contains(stage, "_revise"):
		return "调整这一章的呼吸"
	case stage == "complete":
		return "这一段故事已经完成"
	default:
		return "故事正在继续生长"
	}
}

func chapterLabel(stage string) string {
	parts := strings.Split(stage, "_")
	if len(parts) < 2 {
		return "这一章"
	}
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		return "这一章"
	}
	return fmt.Sprintf("第 %d 章", number)
}

func (server *Server) handleStory(response http.ResponseWriter, request *http.Request) {
	current, ok := server.lookupJob(request.PathValue("id"))
	if !ok {
		writeError(response, http.StatusNotFound, fmt.Errorf("故事不存在"))
		return
	}
	snapshot, err := loadSnapshot(current)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
}

// handleLibrary 将所有已提交项目投影为数字书架。扫描只承认拥有有效 HEAD 的目录，
// 因而失败的半初始化任务和临时工作文件不会出现在用户视野中。
func (server *Server) handleLibrary(response http.ResponseWriter, _ *http.Request) {
	if err := server.syncLibrary(); err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	items := server.libraryJobs()
	snapshots := make([]storySnapshot, 0, len(items))
	for _, current := range items {
		snapshot, err := loadSnapshot(current)
		if err == nil {
			snapshots = append(snapshots, snapshot)
		}
	}
	writeJSON(response, http.StatusOK, snapshots)
}

func (server *Server) libraryJobs() []job {
	server.mu.RLock()
	defer server.mu.RUnlock()
	items := make([]job, 0, len(server.jobs))
	for _, current := range server.jobs {
		copy := *current
		copy.Events = append([]progressEvent(nil), current.Events...)
		items = append(items, copy)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].UpdatedAt.After(items[right].UpdatedAt) })
	return items
}

func (server *Server) lookupJob(id string) (job, bool) {
	if err := server.syncLibrary(); err != nil {
		return job{}, false
	}
	return server.copyJob(id)
}

func (server *Server) copyJob(id string) (job, bool) {
	server.mu.RLock()
	defer server.mu.RUnlock()
	current, ok := server.jobs[id]
	if !ok {
		return job{}, false
	}
	copy := *current
	copy.Events = append([]progressEvent(nil), current.Events...)
	return copy, true
}

type storySnapshot struct {
	ID             string          `json:"id"`
	Status         string          `json:"status"`
	Phase          string          `json:"phase"`
	Error          string          `json:"error,omitempty"`
	Length         string          `json:"length"`
	Title          string          `json:"title,omitempty"`
	Logline        string          `json:"logline,omitempty"`
	CurrentChapter int             `json:"current_chapter"`
	StoryStatus    string          `json:"story_status,omitempty"`
	Running        bool            `json:"running"`
	Chapters       []chapterMeta   `json:"chapters"`
	Events         []progressEvent `json:"events"`
	Idea           string          `json:"idea,omitempty"`
	CreatedAt      time.Time       `json:"created_at,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type chapterMeta struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

func loadSnapshot(current job) (storySnapshot, error) {
	snapshot := storySnapshot{
		ID: current.ID, Status: current.Status, Phase: current.Phase, Error: current.Error,
		Length: current.Length, Running: current.Running, Chapters: []chapterMeta{}, Events: current.Events,
		UpdatedAt: current.UpdatedAt,
	}
	if _, err := os.Stat(filepath.Join(current.Root, "HEAD")); errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	} else if err != nil {
		return snapshot, err
	}

	files := store.New(current.Root)
	project, core, state, err := loadCommittedStory(files)
	if err != nil {
		return snapshot, err
	}
	snapshot.Title, snapshot.Logline = project.Title, core.ExperienceContract.TargetExperience
	snapshot.Idea, snapshot.CreatedAt = project.Idea, project.CreatedAt
	snapshot.CurrentChapter, snapshot.StoryStatus = state.Chapter, state.Status()
	summaries, err := files.LoadLedger()
	if err != nil {
		return snapshot, err
	}
	for _, summary := range summaries {
		snapshot.Chapters = append(snapshot.Chapters, chapterMeta{
			Number: summary.Chapter, Title: summary.Title, Summary: summary.Summary,
		})
	}
	return snapshot, nil
}

func loadCommittedStory(files *store.Store) (story.Project, story.StoryCore, story.State, error) {
	project, err := files.LoadProject()
	if err != nil {
		return story.Project{}, story.StoryCore{}, story.State{}, err
	}
	core, err := files.LoadCore()
	if err != nil {
		return story.Project{}, story.StoryCore{}, story.State{}, err
	}
	state, err := files.LoadState()
	return project, core, state, err
}

func (server *Server) handleChapter(response http.ResponseWriter, request *http.Request) {
	current, ok := server.lookupJob(request.PathValue("id"))
	if !ok {
		writeError(response, http.StatusNotFound, fmt.Errorf("故事不存在"))
		return
	}
	number, err := strconv.Atoi(request.PathValue("number"))
	if err != nil || number < 1 {
		writeError(response, http.StatusBadRequest, fmt.Errorf("章节编号无效"))
		return
	}
	chapter, err := loadChapter(current.Root, number)
	if err != nil {
		writeError(response, http.StatusNotFound, err)
		return
	}
	writeJSON(response, http.StatusOK, chapter)
}

type chapterResponse struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func loadChapter(root string, number int) (chapterResponse, error) {
	files := store.New(root)
	state, err := files.LoadState()
	if err != nil || number > state.Chapter {
		return chapterResponse{}, fmt.Errorf("这一章还没有准备好")
	}
	body, err := files.LoadChapter(number)
	if err != nil || strings.TrimSpace(body) == "" {
		return chapterResponse{}, fmt.Errorf("读取章节失败")
	}
	title := fmt.Sprintf("第 %d 章", number)
	summaries, err := files.LoadLedger()
	if err != nil {
		return chapterResponse{}, fmt.Errorf("读取章节摘要失败")
	}
	for _, summary := range summaries {
		if summary.Chapter == number {
			title = summary.Title
			break
		}
	}
	return chapterResponse{Number: number, Title: title, Body: body}, nil
}

func (server *Server) handleNext(response http.ResponseWriter, request *http.Request) {
	server.startContinuation(response, request.PathValue("id"), 1, "next")
}

func (server *Server) handleComplete(response http.ResponseWriter, request *http.Request) {
	server.startContinuation(response, request.PathValue("id"), 0, "complete")
}

func (server *Server) startContinuation(response http.ResponseWriter, id string, limit int, operation string) {
	current, ok := server.lookupJob(id)
	if !ok {
		writeError(response, http.StatusNotFound, fmt.Errorf("故事不存在"))
		return
	}

	// 是否能够继续取决于正式状态能否读取，不能把前三章试读变成失败后的恢复门槛。
	// 读取错误单独返回，避免把检查点损坏误报为“还在生成”，让用户永远等待。
	state, err := store.New(current.Root).LoadState()
	if err != nil {
		writeError(response, http.StatusConflict, fmt.Errorf("暂时无法继续生长：%w", err))
		return
	}

	// 正式完结优先于试读章数，第一章就完结的短篇也不能再启动。
	if state.Completed {
		writeError(response, http.StatusConflict, fmt.Errorf("故事已经抵达结局"))
		return
	}

	// 逐章生长从第 0 章起就可用；完成全书仍保留读完三章再选择的产品入口。
	if operation == "complete" && state.Chapter < previewChapterCount {
		writeError(response, http.StatusConflict, fmt.Errorf("请先等三章试读完成"))
		return
	}
	if !server.markRunning(id) {
		writeError(response, http.StatusConflict, fmt.Errorf("故事正在生成中"))
		return
	}

	writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted"})
	go server.run(id, operation, func(reporter workflow.Reporter) error {
		return server.runtime.Continue(server.ctx, current.Root, limit, reporter)
	})
}

// markRunning 在锁内检查并占用任务，防止不同页面同时让同一本书继续生长。
func (server *Server) markRunning(id string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	current := server.jobs[id]
	if current.Running {
		return false
	}
	current.Running = true
	current.Status = "writing"
	current.Error = ""
	current.Phase = "故事正在继续生长"
	return true
}

func decodeRequest(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, fmt.Errorf("请求内容无效"))
		return err
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, map[string]string{"error": err.Error()})
}
