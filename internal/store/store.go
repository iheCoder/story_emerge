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

// Store 是一个以项目根目录为边界的文件仓库。
// 它不缓存故事状态：每次读取都回到磁盘，故障恢复时不会依赖旧进程的内存快照。
type Store struct {
	root string
}

// UsageRecord 记录一次成功的模型调用及其成本/耗时。
// 失败请求不在这里记账，避免把“尝试次数”和供应商实际返回的成功结果混为一谈。
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

// New 创建绑定到 root 的文件仓库，不会隐式创建或清空目录。
func New(root string) *Store {
	// Clean 只做路径规范化，不创建目录；目录创建由 Create 控制，
	// 这样 status/run 读取不存在的项目时能明确返回“项目不存在”。
	return &Store{root: filepath.Clean(root)}
}

// Root 返回仓库绑定的规范化根目录。
func (store *Store) Root() string {
	// Root 用于 CLI 或测试展示实际操作的项目目录，调用者不能借此绕过 Store 的路径拼接。
	return store.root
}

// Create 初始化完整目录，最后才写 HEAD，因此失败的初始化不会伪装成可运行项目。
func (store *Store) Create(project story.Project, genesis story.Genesis) error {
	// 初始化按“目录 -> 不可变输入 -> 000 检查点 -> 人类视图 -> HEAD”顺序执行。
	// HEAD 是项目可见性的提交标记，必须最后写，避免半初始化目录被误判为可继续项目。
	// 阶段一：确认目标目录可以安全初始化，拒绝覆盖任何已有项目。
	if err := store.ensureNewRoot(); err != nil {
		return err
	}

	// 阶段二：一次创建所有固定产物目录，后续写入不再隐式扩展布局。
	if err := store.createDirectories(); err != nil {
		return err
	}

	// 阶段三：写入项目配置、Bible 和第 0 章检查点。
	// 这些 JSON 是后续恢复所需的机器事实，必须先于任何可见提交指针存在。
	state := story.NewInitialState(genesis.InitialState, genesis.Outline)

	files := []struct {
		path string
		data any
	}{
		{"project.json", project}, {"story.json", genesis.Bible},
		{outlinePath(0), genesis.Outline},
		{checkpointPath(0), state},
		{readerCheckpointPath(0), genesis.InitialReaderState},
	}
	for _, file := range files {
		if err := store.writeJSON(file.path, file.data); err != nil {
			return err
		}
	}

	// 阶段四：写入原始点子和人读视图，方便用户检查 Architect 的一次性交付。
	if err := store.writeText("brief.md", "# 原始创作点子\n\n"+project.Idea+"\n"); err != nil {
		return err
	}
	if err := store.writeHumanViews(genesis.Bible, state); err != nil {
		return err
	}

	// 阶段五：最后写入 HEAD，正式宣布项目初始化完成。
	return store.writeText("HEAD", "000\n")
}

// LoadProject 读取创建时固化的运行配置。
func (store *Store) LoadProject() (story.Project, error) {
	// project.json 是运行配置的唯一来源；不从环境变量覆盖，保证恢复运行沿用创建时契约。
	var project story.Project
	err := store.readJSON("project.json", &project)
	return project, err
}

// LoadBible 读取全书静态故事圣经。
func (store *Store) LoadBible() (story.StoryBible, error) {
	// Bible 是跨章节的世界观/人物/主线约束，读取失败时不能继续生成正文。
	var bible story.StoryBible
	err := store.readJSON("story.json", &bible)
	return bible, err
}

// LoadState 根据 HEAD 选择并读取当前提交检查点。
func (store *Store) LoadState() (story.State, error) {
	// 先读 HEAD 再读对应 checkpoint，形成“提交指针 -> 不可变快照”的两段式恢复。
	// HEAD 必须是非负整数；任何手工损坏都应立即报错而不是猜测最近文件。
	// 读取 HEAD 提交指针。
	head, err := os.ReadFile(store.path("HEAD"))
	if err != nil {
		return story.State{}, fmt.Errorf("读取 HEAD 失败: %w", err)
	}

	// 严格解析并校验章节编号，不猜测损坏指针的替代值。
	number, err := strconv.Atoi(strings.TrimSpace(string(head)))
	if err != nil || number < 0 {
		return story.State{}, fmt.Errorf("HEAD 内容无效: %q", strings.TrimSpace(string(head)))
	}

	// 读取指针指向的不可变状态快照。
	var state story.State
	err = store.readJSON(checkpointPath(number), &state)
	return state, err
}

// LoadReaderState reads the reader checkpoint at the same committed HEAD.
func (store *Store) LoadReaderState() (story.ReaderState, error) {
	number, err := store.loadHEAD()
	if err != nil {
		return story.ReaderState{}, err
	}
	var state story.ReaderState
	err = store.readJSON(readerCheckpointPath(number), &state)
	return state, err
}

// LoadOutline reads the immutable outline version referenced by State.
func (store *Store) LoadOutline(version int) (story.StoryOutline, error) {
	var outline story.StoryOutline
	err := store.readJSON(outlinePath(version), &outline)
	return outline, err
}

func (store *Store) loadHEAD() (int, error) {
	head, err := os.ReadFile(store.path("HEAD"))
	if err != nil {
		return 0, fmt.Errorf("读取 HEAD 失败: %w", err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(head)))
	if err != nil || number < 0 {
		return 0, fmt.Errorf("HEAD 内容无效: %q", strings.TrimSpace(string(head)))
	}
	return number, nil
}

// LoadChapter 读取已提交章节；不存在的正章节号表示尚未提交。
func (store *Store) LoadChapter(number int) (string, error) {
	// 章节文件缺失被视为“尚未提交”，而其他 IO 错误继续暴露，避免静默吞掉磁盘故障。

	// 处理第 0 章或负数请求，它们没有可读取正文。
	if number < 1 {
		return "", nil
	}

	// 读取已提交章节，并区分“尚未存在”和真实 IO 故障。
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
	// 导出只投影 Bible 标题和 HEAD 之前的章节正文，故障现场 .work 与未提交产物天然被排除。
	// 读取 Bible 和当前 HEAD 状态，确定标题与导出边界。
	bible, err := store.LoadBible()
	if err != nil {
		return "", err
	}
	state, err := store.LoadState()
	if err != nil {
		return "", err
	}

	// 按章节顺序拼接已提交正文；缺失章节直接暴露错误。
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

	// 以原子写入方式刷新导出文件。
	if err := store.writeText("manuscript.md", manuscript.String()); err != nil {
		return "", err
	}
	return store.path("manuscript.md"), nil
}

// SaveWorking 将中间产物保存到 .work 诊断区，不改变正式提交指针，
// 便于定位失败原因或人工接管。
func (store *Store) SaveWorking(number int, name string, data any) error {
	// .work 是可诊断的临时区：保存草稿、状态与审核意见等中间结果，
	// 但不改变 HEAD，因此人工查看或下次重跑都不会把草稿当成正式剧情。
	// 根据章节号和产物名计算诊断文件路径。
	path := filepath.Join(".work", fmt.Sprintf("%03d-%s", number, name))

	// 字符串按 Markdown/文本保存，其余值按缩进 JSON 保存。
	if text, ok := data.(string); ok {
		return store.writeText(path, text)
	}
	return store.writeJSON(path, data)
}

// CommitChapter 把本章所有产物写好后才推进 HEAD。
// 即使进程在最后一步前退出，旧 HEAD 仍指向完整、可读取的上一章。
func (store *Store) CommitChapter(chapter string, delta story.StateDelta, review story.CanonReview, reader story.ReaderState, state story.State, outline story.StoryOutline, replanned bool) error {
	// 依次写正文、状态差量、Canon 结果、Reader 状态和人类视图，最后原子替换 HEAD。
	// 任一步失败都会保留旧 HEAD；代价是可能留下可清理的孤儿文件，但不会破坏可恢复性。
	number := state.Chapter
	if err := validateCommit(delta, review, reader, state, outline); err != nil {
		return err
	}
	if err := store.writeChapterArtifacts(number, chapter, delta, review, reader); err != nil {
		return err
	}
	if replanned {
		if err := store.writeJSON(outlinePath(outline.Version), outline); err != nil {
			return err
		}
	}
	if err := store.writeJSON(checkpointPath(number), state); err != nil {
		return err
	}

	// 刷新给人阅读的状态视图。
	bible, err := store.LoadBible()
	if err != nil {
		return err
	}
	if err := store.writeHumanViews(bible, state); err != nil {
		return err
	}

	// 所有产物成功后才原子推进 HEAD。
	return store.writeText("HEAD", fmt.Sprintf("%03d\n", number))
}

func validateCommit(delta story.StateDelta, review story.CanonReview, reader story.ReaderState, state story.State, outline story.StoryOutline) error {
	number := state.Chapter
	if delta.Chapter != number || delta.Summary.Number != number {
		return fmt.Errorf("状态增量与候选检查点章节不一致: %d/%d/%d", delta.Chapter, delta.Summary.Number, number)
	}
	if !review.Passed {
		return fmt.Errorf("正典检查未通过，不能提交第 %d 章", number)
	}
	if err := story.ValidateReaderState(reader, number); err != nil {
		return fmt.Errorf("读者检查点无效: %w", err)
	}
	if state.OutlineVersion != outline.Version {
		return fmt.Errorf("大纲版本与候选检查点不一致: %d/%d", outline.Version, state.OutlineVersion)
	}
	return nil
}

func (store *Store) writeChapterArtifacts(number int, chapter string, delta story.StateDelta, review story.CanonReview, reader story.ReaderState) error {
	if err := store.writeText(chapterPath(number), chapter); err != nil {
		return err
	}
	if err := store.writeJSON(deltaPath(number), delta); err != nil {
		return err
	}
	if err := store.writeJSON(canonReviewPath(number), review); err != nil {
		return err
	}
	if err := store.writeJSON(readerCheckpointPath(number), reader); err != nil {
		return err
	}
	return nil
}

// AppendUsage 追加一条成功调用记录到 usage.jsonl。
func (store *Store) AppendUsage(stage string, result llm.Result) error {
	// 使用 JSON Lines 追加而非覆盖，便于长时间运行逐调用审计且无需加载全部历史到内存。

	// 将模型结果投影为不含原文的成本审计记录。
	record := UsageRecord{
		At:    time.Now().UTC(),
		Stage: stage, Model: result.Model, ResponseID: result.ResponseID,
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		TotalTokens: result.Usage.TotalTokens, CachedTokens: result.Usage.CachedTokens,
		DurationMS: result.Duration.Milliseconds(),
	}

	// 以追加模式写入，一次调用占一行。
	file, err := os.OpenFile(store.path("usage.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 usage.jsonl 失败: %w", err)
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(record)
}

// CountUsage 统计 usage.jsonl 中已有的成功调用行数。
func (store *Store) CountUsage() (int, error) {
	// 调用数按行计数，与 AppendUsage 的一行一条记录契约一致；空文件等价于零次成功调用。

	// 打开已有用量日志；文件不存在代表新项目。
	file, err := os.Open(store.path("usage.jsonl"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// 逐行统计，避免长时间运行时一次性加载全部日志。
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// writeHumanViews 根据机器状态刷新给人阅读的 story.md 和 status.md。
func (store *Store) writeHumanViews(bible story.StoryBible, state story.State) error {
	// story.md/status.md 是给人读的派生视图，真实事实仍以 JSON 检查点和 HEAD 为准。

	// 刷新全书 Bible 视图。
	if err := store.writeText("story.md", story.RenderStory(bible)); err != nil {
		return err
	}

	// 刷新当前状态视图。
	return store.writeText("status.md", story.RenderStatus(bible, state))
}

// ensureNewRoot 确保初始化目标是不存在或空目录。
func (store *Store) ensureNewRoot() error {
	// new 只接受不存在或空目录，防止误把已有项目当成新项目覆盖。

	// 检查目标目录是否已经存在。
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		// 不存在时创建目录；后续 Create 会继续建立子目录和文件。
		return os.MkdirAll(store.root, 0o755)
	}
	if err != nil {
		return err
	}

	// 已有目录只能为空，拒绝覆盖任何历史项目。
	if len(entries) > 0 {
		return fmt.Errorf("输出目录已存在且非空: %s", store.root)
	}
	return nil
}

// createDirectories 建立项目产物的固定目录布局。
func (store *Store) createDirectories() error {
	// 目录集合对应状态机的产物类型；集中创建让后续写入无需在每个阶段重复判断目录。
	for _, directory := range []string{"chapters", "deltas", "canon-reviews", "reader-checkpoints", "outlines", "checkpoints", ".work"} {
		if err := os.MkdirAll(store.path(directory), 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", directory, err)
		}
	}
	return nil
}

// readJSON 统一读取并解码仓库内 JSON 文件。
func (store *Store) readJSON(relative string, target any) error {
	// 所有 JSON 读取统一补充相对文件名，错误信息才能直接对应项目内的损坏产物。

	// 打开目标文件。
	file, err := os.Open(store.path(relative))
	if err != nil {
		return fmt.Errorf("打开 %s 失败: %w", relative, err)
	}
	defer file.Close()

	// 解码到调用方提供的领域对象。
	if err := json.NewDecoder(file).Decode(target); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", relative, err)
	}
	return nil
}

// writeJSON 将值格式化后以原子文本写入仓库。
func (store *Store) writeJSON(relative string, value any) error {
	// 缩进 JSON 是刻意的可维护性选择：状态文件需要被人审阅和在故障时手工修复。

	// 序列化领域对象。
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", relative, err)
	}

	// 复用原子文本写入，避免半截 JSON 被读取。
	return store.writeText(relative, string(data)+"\n")
}

// writeText 将任意文本写入临时文件并原子替换目标文件。
func (store *Store) writeText(relative, content string) error {
	// 通过同目录临时文件 + finishAtomicWrite 写入，确保读者不会看到半截 JSON/Markdown。

	// 确保目标父目录存在并创建同目录临时文件。
	target := store.path(relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".story-emerge-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	// 完整写入后再原子替换正式文件。
	return finishAtomicWrite(temporary, target, []byte(content))
}

// finishAtomicWrite 完成临时文件的写入、落盘、关闭和原子改名。
func finishAtomicWrite(file *os.File, target string, content []byte) error {
	// 先完整写入并 Sync，再关闭并 Rename；同目录 Rename 在本地文件系统上提供原子替换语义。
	// 临时文件无论成功失败都会清理，避免重复运行留下大量不可见垃圾。
	temporary := file.Name()
	defer os.Remove(temporary)

	// 写入并同步临时文件，确保后续替换使用完整内容。
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

	// 关闭后原子改名，读者只会看到旧文件或完整新文件。
	return os.Rename(temporary, target)
}

// path 把项目内相对路径绑定到仓库根目录。
func (store *Store) path(relative string) string {
	// 所有存储访问都从 root 拼接相对路径，避免业务层散落 filepath.Join 导致边界不一致。
	return filepath.Join(store.root, relative)
}

// chapterPath 返回章节 Markdown 的固定三位编号路径。
func chapterPath(number int) string {
	// 三位数字命名既保持目录字典序，也让章节编号与 HEAD/日志中的编号一致。
	return filepath.Join("chapters", fmt.Sprintf("%03d.md", number))
}

func canonReviewPath(number int) string {
	return filepath.Join("canon-reviews", fmt.Sprintf("%03d.json", number))
}
func readerCheckpointPath(number int) string {
	return filepath.Join("reader-checkpoints", fmt.Sprintf("%03d.json", number))
}
func outlinePath(version int) string {
	return filepath.Join("outlines", fmt.Sprintf("%03d.json", version))
}

// deltaPath 返回章节状态增量 JSON 的固定编号路径。
func deltaPath(number int) string {
	// delta 是本章对长期状态的最小变更集，是重放和定位状态漂移的依据。
	return filepath.Join("deltas", fmt.Sprintf("%03d.json", number))
}

// checkpointPath 返回章节提交后完整状态的固定编号路径。
func checkpointPath(number int) string {
	// checkpoint 保存提交后的完整状态；按章节编号不可变落盘，HEAD 只负责选择当前版本。
	return filepath.Join("checkpoints", fmt.Sprintf("%03d.json", number))
}
