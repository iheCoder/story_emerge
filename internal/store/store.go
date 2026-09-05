// Package store 使用 HEAD 作为唯一提交边界。正文、提取记录和检查点写全后才推进 HEAD。
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/story"
)

type Store struct{ root string }
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

func New(root string) *Store      { return &Store{root: filepath.Clean(root)} }
func (store *Store) Root() string { return store.root }

// Prepare 在调用模型前拒绝覆盖已有项目，并原样保存 User Idea。
// 此时没有 HEAD；初始化中断的目录只提供诊断证据，不会被书架当成正式故事。
func (store *Store) Prepare(project story.Project) error {
	if err := story.ValidateProject(project); err != nil {
		return err
	}
	entries, err := os.ReadDir(store.root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("输出目录已存在且非空: %s", store.root)
	}
	for _, dir := range []string{"chapters", "commits", "checkpoints", "direction-reviews", ".work"} {
		if err := os.MkdirAll(store.path(dir), 0755); err != nil {
			return err
		}
	}
	return store.writeJSON("project.json", project)
}

// CommitGenesis 仅提交一次作品核心与第 0 章初态；后续章节没有改写 Core 的入口。
func (store *Store) CommitGenesis(genesis story.Genesis) error {
	if _, err := os.Stat(store.path("HEAD")); err == nil {
		return fmt.Errorf("项目已经初始化")
	} else if !os.IsNotExist(err) {
		return err
	}

	// 存储层也执行一次幂等归一化，保证绕过 workflow 的调用仍不会写出无 ID 的人物状态。
	// 已经解析过的 ID 会保持不变，因此 workflow 保存的结果与最终 checkpoint 完全一致。
	initial, err := story.ResolveInitialStateItemIDs(genesis.InitialStoryState)
	if err != nil {
		return err
	}
	genesis.InitialStoryState = initial

	if err := story.ValidateGenesis(genesis); err != nil {
		return err
	}
	project, err := store.LoadProject()
	if err != nil {
		return err
	}
	project.Title = genesis.Title
	if err := store.writeJSON("project.json", project); err != nil {
		return err
	}
	if err := store.writeJSON("story-core.json", genesis.StoryCore); err != nil {
		return err
	}
	if err := store.writeJSON(checkpointPath(0), story.NewInitialState(genesis)); err != nil {
		return err
	}
	return store.writeText("HEAD", "000\n")
}
func (store *Store) LoadProject() (story.Project, error) {
	var project story.Project
	if err := store.readJSON("project.json", &project); err != nil {
		return project, err
	}
	return project, story.ValidateProject(project)
}
func (store *Store) LoadCore() (story.StoryCore, error) {
	var core story.StoryCore
	if err := store.readJSON("story-core.json", &core); err != nil {
		return core, err
	}
	return core, story.ValidateCore(core)
}
func (store *Store) loadHEAD() (int, error) {
	data, err := os.ReadFile(store.path("HEAD"))
	if err != nil {
		return 0, fmt.Errorf("读取 HEAD 失败: %w", err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || number < 0 {
		return 0, fmt.Errorf("HEAD 内容无效")
	}
	return number, nil
}
func (store *Store) LoadState() (story.State, error) {
	number, err := store.loadHEAD()
	if err != nil {
		return story.State{}, err
	}
	var state story.State
	if err := store.readJSON(checkpointPath(number), &state); err != nil {
		return state, err
	}
	if state.Chapter != number {
		return state, fmt.Errorf("检查点章节与 HEAD 不一致")
	}
	return state, story.ValidateState(state)
}

// LoadChapter 严格限制在已提交历史内，磁盘上的孤儿正文不能越过 HEAD 被读取。
func (store *Store) LoadChapter(number int) (string, error) {
	if number == 0 {
		return "", nil
	}
	head, err := store.loadHEAD()
	if err != nil {
		return "", err
	}
	if number < 1 || number > head {
		return "", fmt.Errorf("第 %d 章尚未提交", number)
	}
	data, err := os.ReadFile(store.path(chapterPath(number)))
	if err != nil {
		return "", fmt.Errorf("读取第 %d 章失败: %w", number, err)
	}
	return string(data), nil
}

// LoadLedger 直接投影正式提交记录，不再维护另一份可能与 HEAD 不一致的全局摘要文件。
func (store *Store) LoadLedger() ([]story.LedgerEntry, error) {
	head, err := store.loadHEAD()
	if err != nil {
		return nil, err
	}
	entries := make([]story.LedgerEntry, 0, head)
	for number := 1; number <= head; number++ {
		var commit story.ChapterCommit
		if err := store.readJSON(commitPath(number), &commit); err != nil {
			return nil, err
		}
		if commit.Chapter != number || commit.Review.ChapterDecision != story.EditorAccept {
			return nil, fmt.Errorf("第 %d 章提交记录无效", number)
		}
		entries = append(entries, story.LedgerEntry{Chapter: number, Title: commit.Title, Summary: commit.Result.ChapterSummary})
	}
	return entries, nil
}

// LoadCommit 只读取 HEAD 范围内的正式章节提交。Director 用它恢复最后一章随 ACCEPT
// 一起保存的提前复查请求，不能从 .work 中读取失败稿件的评审意见。
func (store *Store) LoadCommit(number int) (story.ChapterCommit, error) {
	head, err := store.loadHEAD()
	if err != nil {
		return story.ChapterCommit{}, err
	}
	if number < 1 || number > head {
		return story.ChapterCommit{}, fmt.Errorf("第 %d 章尚未提交", number)
	}
	var commit story.ChapterCommit
	if err := store.readJSON(commitPath(number), &commit); err != nil {
		return story.ChapterCommit{}, err
	}
	if commit.Chapter != number {
		return story.ChapterCommit{}, fmt.Errorf("第 %d 章提交记录章节号无效", number)
	}
	return commit, nil
}

// CommitChapter 自行从 HEAD 计算下一份状态，调用者不能传入一个与补丁不一致的快照。
// 任一文件写入失败时旧 HEAD 保持不变，工作流下次从正式历史生成尚未提交的章节。
func (store *Store) CommitChapter(chapter string, commit story.ChapterCommit) (story.State, error) {
	current, err := store.LoadState()
	if err != nil {
		return story.State{}, err
	}

	// commit.json 是 checkpoint 的可重放依据，必须保存已经补全稳定 ID 的原子 Patch。
	// 这层防御使直接调用 Store 的代码与正常 workflow 采用完全相同的 reducer 输入。
	commit.Result.StatePatch, err = story.ResolveStatePatchIDs(current.Story, commit.Result.StatePatch)
	if err != nil {
		return story.State{}, err
	}
	next, err := story.ApplyChapter(current, chapter, commit)
	if err != nil {
		return story.State{}, err
	}
	if err := store.writeText(chapterPath(next.Chapter), chapter); err != nil {
		return story.State{}, err
	}
	if err := store.writeJSON(commitPath(next.Chapter), commit); err != nil {
		return story.State{}, err
	}
	if err := store.writeJSON(checkpointPath(next.Chapter), next); err != nil {
		return story.State{}, err
	}
	if err := store.writeText("HEAD", fmt.Sprintf("%03d\n", next.Chapter)); err != nil {
		return story.State{}, err
	}
	return next, nil
}

// CommitDirectionReview 在当前 HEAD 对应的检查点上原子更新 Direction，不创建新章节也不移动 HEAD。
// 先写审计记录再替换检查点；若检查点写入失败，旧 Direction 仍是正式状态，下次运行会重新复查。
func (store *Store) CommitDirectionReview(review story.DirectionReview) (story.State, error) {
	current, err := store.LoadState()
	if err != nil {
		return story.State{}, err
	}
	next, err := story.ApplyDirectionReview(current, review)
	if err != nil {
		return story.State{}, err
	}

	if err := store.writeJSON(directionReviewPath(review.AfterChapter), review); err != nil {
		return story.State{}, err
	}
	if err := store.writeJSON(checkpointPath(current.Chapter), next); err != nil {
		return story.State{}, err
	}
	return next, nil
}

func (store *Store) SaveWorking(number int, name string, data any) error {
	path := filepath.Join(".work", fmt.Sprintf("%03d-%s", number, name))
	if text, ok := data.(string); ok {
		return store.writeText(path, text)
	}
	return store.writeJSON(path, data)
}
func (store *Store) ExportManuscript() (string, error) {
	project, err := store.LoadProject()
	if err != nil {
		return "", err
	}
	state, err := store.LoadState()
	if err != nil {
		return "", err
	}
	var manuscript strings.Builder
	fmt.Fprintf(&manuscript, "# %s\n\n", project.Title)
	for number := 1; number <= state.Chapter; number++ {
		chapter, err := store.LoadChapter(number)
		if err != nil {
			return "", err
		}
		manuscript.WriteString(strings.TrimSpace(chapter) + "\n\n")
	}
	if err := store.writeText("manuscript.md", manuscript.String()); err != nil {
		return "", err
	}
	return store.path("manuscript.md"), nil
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

// readJSON 统一读取并解码仓库内 JSON 文件。
func (store *Store) readJSON(relative string, target any) error {
	// 所有 JSON 读取统一补充相对文件名，错误信息才能直接对应项目内的损坏产物。

	// 打开目标文件。
	file, err := os.Open(store.path(relative))
	if err != nil {
		return fmt.Errorf("打开 %s 失败: %w", relative, err)
	}
	defer file.Close()

	// 拒绝未知字段，防止损坏或不匹配的文件被解码成不完整状态后继续写作。
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", relative, err)
	}

	// Decode 一次只消费一个 JSON 值；再读一次确认已到文件结尾，拒绝追加对象或尾部垃圾。
	// 首个对象解析成功不代表整个状态文件完整，不能忽略尾部损坏后继续生成。
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("解析 %s 失败: JSON 后存在多余内容", relative)
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
func chapterPath(number int) string    { return fmt.Sprintf("chapters/%03d.md", number) }
func commitPath(number int) string     { return fmt.Sprintf("commits/%03d.json", number) }
func checkpointPath(number int) string { return fmt.Sprintf("checkpoints/%03d.json", number) }
func directionReviewPath(number int) string {
	return fmt.Sprintf("direction-reviews/%03d.json", number)
}
