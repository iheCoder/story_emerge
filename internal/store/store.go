// Package store 负责小说项目的文件持久化。
// 提交协议的核心是：先写不可变产物，最后原子替换 HEAD；读取者永远只相信 HEAD。
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

type Store struct {
	root string
}

type UsageRecord struct {
	At           time.Time `json:"at"`
	Stage        string    `json:"stage"`
	Model        string    `json:"model"`
	ResponseID   string    `json:"response_id"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	CachedTokens int       `json:"cached_tokens"`
	DurationMS   int64     `json:"duration_ms"`
}

func New(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

func (store *Store) Root() string {
	return store.root
}

// Create 初始化完整目录，最后才写 HEAD，因此失败的初始化不会伪装成可运行项目。
func (store *Store) Create(project story.Project, genesis story.Genesis) error {
	if err := store.ensureNewRoot(); err != nil {
		return err
	}
	if err := store.createDirectories(); err != nil {
		return err
	}
	state := story.NewInitialState(genesis.InitialState)
	files := []struct {
		path string
		data any
	}{
		{"project.json", project}, {"story.json", genesis.Bible},
		{filepath.Join("checkpoints", "000.json"), state},
	}
	for _, file := range files {
		if err := store.writeJSON(file.path, file.data); err != nil {
			return err
		}
	}
	if err := store.writeText("brief.md", "# 原始创作点子\n\n"+project.Idea+"\n"); err != nil {
		return err
	}
	if err := store.writeHumanViews(genesis.Bible, state); err != nil {
		return err
	}
	return store.writeText("HEAD", "000\n")
}

func (store *Store) LoadProject() (story.Project, error) {
	var project story.Project
	err := store.readJSON("project.json", &project)
	return project, err
}

func (store *Store) LoadBible() (story.StoryBible, error) {
	var bible story.StoryBible
	err := store.readJSON("story.json", &bible)
	return bible, err
}

func (store *Store) LoadState() (story.State, error) {
	head, err := os.ReadFile(store.path("HEAD"))
	if err != nil {
		return story.State{}, fmt.Errorf("读取 HEAD 失败: %w", err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(head)))
	if err != nil || number < 0 {
		return story.State{}, fmt.Errorf("HEAD 内容无效: %q", strings.TrimSpace(string(head)))
	}
	var state story.State
	err = store.readJSON(checkpointPath(number), &state)
	return state, err
}

func (store *Store) LoadChapter(number int) (string, error) {
	if number < 1 {
		return "", nil
	}
	content, err := os.ReadFile(store.path(chapterPath(number)))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取第 %d 章失败: %w", number, err)
	}
	return string(content), nil
}

// ExportManuscript 按 HEAD 指向的已提交历史合并完整书稿。
// `.work` 中未通过的草稿不会进入导出文件。
func (store *Store) ExportManuscript() (string, error) {
	bible, err := store.LoadBible()
	if err != nil {
		return "", err
	}
	state, err := store.LoadState()
	if err != nil {
		return "", err
	}
	var manuscript strings.Builder
	fmt.Fprintf(&manuscript, "# %s\n\n", bible.Title)
	for number := 1; number <= state.Chapter; number++ {
		chapter, err := store.LoadChapter(number)
		if err != nil {
			return "", err
		}
		manuscript.WriteString(strings.TrimSpace(chapter))
		manuscript.WriteString("\n\n")
	}
	if err := store.writeText("manuscript.md", manuscript.String()); err != nil {
		return "", err
	}
	return store.path("manuscript.md"), nil
}

// SaveWorking 保存尚未通过编辑审核的现场，便于定位失败原因或人工接管。
func (store *Store) SaveWorking(number int, name string, data any) error {
	path := filepath.Join(".work", fmt.Sprintf("%03d-%s", number, name))
	if text, ok := data.(string); ok {
		return store.writeText(path, text)
	}
	return store.writeJSON(path, data)
}

// CommitChapter 把本章所有产物写好后才推进 HEAD。
// 即使进程在最后一步前退出，旧 HEAD 仍指向完整、可读取的上一章。
func (store *Store) CommitChapter(plan story.ChapterPlan, chapter string, delta story.StateDelta, review story.Review, state story.State) error {
	number := state.Chapter
	if plan.Number != number {
		return fmt.Errorf("计划章节 %d 与状态章节 %d 不一致", plan.Number, number)
	}
	if err := store.writeJSON(planPath(number), plan); err != nil {
		return err
	}
	if err := store.writeText(chapterPath(number), chapter); err != nil {
		return err
	}
	if err := store.writeJSON(deltaPath(number), delta); err != nil {
		return err
	}
	if err := store.writeJSON(reviewPath(number), review); err != nil {
		return err
	}
	if err := store.writeJSON(checkpointPath(number), state); err != nil {
		return err
	}
	bible, err := store.LoadBible()
	if err != nil {
		return err
	}
	if err := store.writeHumanViews(bible, state); err != nil {
		return err
	}
	return store.writeText("HEAD", fmt.Sprintf("%03d\n", number))
}

func (store *Store) AppendUsage(stage string, result llm.Result) error {
	record := UsageRecord{
		At:    time.Now().UTC(),
		Stage: stage, Model: result.Model, ResponseID: result.ResponseID,
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		TotalTokens: result.Usage.TotalTokens, CachedTokens: result.Usage.CachedTokens,
		DurationMS: result.Duration.Milliseconds(),
	}
	file, err := os.OpenFile(store.path("usage.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 usage.jsonl 失败: %w", err)
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(record)
}

func (store *Store) CountUsage() (int, error) {
	file, err := os.Open(store.path("usage.jsonl"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func (store *Store) writeHumanViews(bible story.StoryBible, state story.State) error {
	if err := store.writeText("story.md", story.RenderStory(bible)); err != nil {
		return err
	}
	return store.writeText("status.md", story.RenderStatus(bible, state))
}

func (store *Store) ensureNewRoot() error {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return os.MkdirAll(store.root, 0o755)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("输出目录已存在且非空: %s", store.root)
	}
	return nil
}

func (store *Store) createDirectories() error {
	for _, directory := range []string{"chapters", "plans", "deltas", "reviews", "checkpoints", ".work"} {
		if err := os.MkdirAll(store.path(directory), 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", directory, err)
		}
	}
	return nil
}

func (store *Store) readJSON(relative string, target any) error {
	file, err := os.Open(store.path(relative))
	if err != nil {
		return fmt.Errorf("打开 %s 失败: %w", relative, err)
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(target); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", relative, err)
	}
	return nil
}

func (store *Store) writeJSON(relative string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", relative, err)
	}
	return store.writeText(relative, string(data)+"\n")
}

func (store *Store) writeText(relative, content string) error {
	target := store.path(relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".story-emerge-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	return finishAtomicWrite(temporary, target, []byte(content))
}

func finishAtomicWrite(file *os.File, target string, content []byte) error {
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func (store *Store) path(relative string) string {
	return filepath.Join(store.root, relative)
}

func chapterPath(number int) string {
	return filepath.Join("chapters", fmt.Sprintf("%03d.md", number))
}

func planPath(number int) string {
	return filepath.Join("plans", fmt.Sprintf("%03d.json", number))
}

func reviewPath(number int) string {
	return filepath.Join("reviews", fmt.Sprintf("%03d.json", number))
}

func deltaPath(number int) string {
	return filepath.Join("deltas", fmt.Sprintf("%03d.json", number))
}

func checkpointPath(number int) string {
	return filepath.Join("checkpoints", fmt.Sprintf("%03d.json", number))
}
