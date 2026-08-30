package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/webapp"
	"story_emerge/internal/workflow"
)

// main 建立可取消的进程上下文，并把子命令错误转换为非零退出码。
func main() {
	// CLI 本身只负责进程级生命周期（信号、退出码和错误展示）；
	// 具体的小说业务交给 workflow.Engine，避免入口函数承担状态机细节。
	// 建立可被 Ctrl-C/SIGTERM 取消的根上下文。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 执行子命令并把业务错误转换为非零退出码。
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

// execute 根据命令名分派 CLI；它不直接实现小说业务，便于单测时注入参数调用。
func execute(ctx context.Context, arguments []string) error {
	// 将第一个参数解释为“用户意图”，其余参数交给对应子命令解析。
	// 这样每条命令都有独立的 flag 集合，新增命令时不会污染已有参数。
	// 没有子命令时展示帮助，不进入任何文件或模型操作。
	if len(arguments) == 0 {
		printUsage()
		return nil
	}

	// 根据命令名转交给对应处理器；处理器各自负责参数校验和业务副作用。
	switch arguments[0] {
	case "new":
		return runNew(ctx, arguments[1:])
	case "run":
		return runNovel(ctx, arguments[1:])
	case "status":
		return showStatus(arguments[1:])
	case "export":
		return exportNovel(arguments[1:])
	case "serve":
		return serveWeb(ctx, arguments[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("未知命令 %q；可用命令：new、run、status、export、serve", arguments[0])
	}
}

// serveWeb 启动本地 Web 产品入口，并在进程信号到来时等待 HTTP 连接优雅退出。
func serveWeb(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("addr", "127.0.0.1:8787", "Web 监听地址")
	dataRoot := flags.String("data", "novels", "小说项目保存目录")
	provider := flags.String("provider", "auto", "模型提供商：auto、deepseek 或 openai")
	model := flags.String("model", "", "覆盖提供商默认模型")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	selectedProvider, err := resolveWebProvider(*provider)
	if err != nil {
		return err
	}
	runtime, err := webapp.NewWorkflowRuntime(selectedProvider, *model)
	if err != nil {
		return err
	}
	application, err := webapp.NewServer(ctx, *dataRoot, runtime)
	if err != nil {
		return err
	}
	return listenWeb(ctx, *address, application.Handler())
}

// resolveWebProvider 让默认 Web 启动命令使用当前已经配置好的模型密钥。
func resolveWebProvider(provider string) (string, error) {
	if provider != "auto" {
		return provider, nil
	}
	if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) != "" {
		return "deepseek", nil
	}
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
		return "openai", nil
	}
	return "", fmt.Errorf("缺少 DEEPSEEK_API_KEY 或 OPENAI_API_KEY")
}

// listenWeb 承担监听和关闭时序，业务 handler 不感知进程信号。
func listenWeb(ctx context.Context, address string, handler http.Handler) error {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	fmt.Printf("story-emerge Web 已启动：http://%s\n", address)

	select {
	case err := <-errors:
		if !errorsIsServerClosed(err) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func errorsIsServerClosed(err error) bool {
	return err == http.ErrServerClosed
}

// runNew 读取创作点子、解析模型配置并初始化一个全新的小说项目。
func runNew(ctx context.Context, arguments []string) error {
	// new 的职责是把人类可读的点子和运行配置落成一个可恢复的项目，
	// 初始化阶段（生成 Bible/初始状态）仍由 Engine 统一执行。

	// 解析创建项目所需的命令行参数。
	// 所有默认值在这里一次确定，随后写入 project.json，保证项目可复现。
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	ideaFile := flags.String("idea-file", "", "小说点子 Markdown 文件")
	output := flags.String("out", "", "小说项目输出目录")
	name := flags.String("name", "", "项目名称；默认使用输出目录名")
	provider := flags.String("provider", "deepseek", "模型提供商：deepseek 或 openai")
	model := flags.String("model", "", "覆盖提供商默认模型")
	chapters := flags.Int("chapters", 12, "目标章节数")
	minChars := flags.Int("min-chars", 2200, "每章最少汉字数")
	maxChars := flags.Int("max-chars", 2800, "每章目标最大汉字数")
	maxCalls := flags.Int("max-calls", 90, "整个项目允许的逻辑模型调用数")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	// 读取点子并规范化输出路径。
	// 这一步先于模型配置，避免缺少本地输入时浪费任何模型调用。
	idea, root, err := readNewInputs(*ideaFile, *output)
	if err != nil {
		return err
	}

	// 解析模型提供商和密钥。
	// 密钥只从环境变量进入内存，不随项目配置落盘。
	config, err := llm.ConfigFromEnv(*provider, *model)
	if err != nil {
		return err
	}

	// 组装项目契约和工作流引擎。
	// Engine 统一负责状态机，CLI 不直接参与章节生成细节。
	project := newProject(*name, root, idea, config, *chapters, *minChars, *maxChars, *maxCalls)
	engine, err := newEngine(config, store.New(root), project.MaxCalls)
	if err != nil {
		return err
	}

	// 调用总导演建立 Bible 和第 0 章状态。
	// 只有 Initialize 成功，输出目录才会拥有可继续运行的 HEAD。
	_, err = engine.Initialize(ctx, project)
	return err
}

// runNovel 从项目 HEAD 恢复状态并按 limit 继续生成章节。
func runNovel(ctx context.Context, arguments []string) error {
	// run 永远从磁盘上的 project.json 和 HEAD 恢复，不把进度依赖在进程内存里。
	// 因而进程中断后再次执行同一命令，会从最后一次提交的章节继续。

	// 解析并校验项目路径。
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	limit := flags.Int("chapters", 0, "本次最多写几章；0 表示写到项目目标")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}

	// 从磁盘读取项目契约。
	// 运行时使用 project.json 中的 provider/model，而不是用当前命令行默认值覆盖历史配置。
	files := store.New(*root)
	project, err := files.LoadProject()
	if err != nil {
		return err
	}

	// 根据项目契约加载模型客户端。
	config, err := llm.ConfigFromEnv(project.Provider, project.Model)
	if err != nil {
		return err
	}

	// 创建可恢复引擎并继续章节状态机。
	engine, err := newEngine(config, files, project.MaxCalls)
	if err != nil {
		return err
	}

	// limit 只限制本次运行步数，不会修改项目的目标章节数。
	return engine.Run(ctx, *limit)
}

// showStatus 打印项目最近一次提交的状态快照。
func showStatus(arguments []string) error {
	// status 是只读诊断命令：直接展示 Engine 写入的状态快照，
	// 不重新读取模型或推导状态，保证查看进度不会产生副作用。

	// 解析并校验项目路径。
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}

	// 读取已由 Engine 生成的状态视图并原样输出。
	content, err := os.ReadFile(filepath.Join(*root, "status.md"))
	if err != nil {
		return err
	}
	fmt.Print(string(content))
	return nil
}

// exportNovel 将已提交章节合并为可阅读的 manuscript.md。
func exportNovel(arguments []string) error {
	// export 只导出 HEAD 指向的已提交章节；工作目录中的半成品不会混入正文。

	// 解析并校验项目路径。
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}

	// 由 Store 按 HEAD 投影已提交章节。
	path, err := store.New(*root).ExportManuscript()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

// readNewInputs 校验 new 的文件参数，并返回去空白的点子和绝对输出路径。
func readNewInputs(ideaFile, output string) (string, string, error) {
	// 在真正创建目录前先校验两个必需输入，并将输出目录规范化为绝对路径。
	// 绝对路径能避免从不同工作目录运行 CLI 时产生两份看似相同的项目。

	// 确认输入参数存在。
	if strings.TrimSpace(ideaFile) == "" || strings.TrimSpace(output) == "" {
		return "", "", fmt.Errorf("new 必须提供 --idea-file 和 --out")
	}

	// 读取用户原始点子，保留其内容作为项目审计材料。
	content, err := os.ReadFile(ideaFile)
	if err != nil {
		return "", "", fmt.Errorf("读取创作点子失败: %w", err)
	}

	// 把输出目录转换为绝对路径，交给 Store 做后续创建和写入。
	absolute, err := filepath.Abs(output)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(content)), absolute, nil
}

// newProject 把 CLI 参数固化为项目契约，后续运行不再依赖本次命令行进程。
func newProject(
	name, root, idea string,
	config llm.Config,
	chapters, minChars, maxChars, maxCalls int,
) story.Project {
	// Project 保存后会成为后续所有阶段的契约：章节目标、字数窗口、模型和调用预算
	// 都从这里读取，因此默认值只能在项目创建时决定，运行中不再隐式改变。
	// 未提供项目名时从输出目录推导稳定名称。
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(root)
	}

	// 组装并返回会写入 project.json 的不可变项目契约。
	return story.Project{
		Version: story.FormatVersion, Name: name, Idea: idea,
		Provider: config.Provider, Model: config.Model,
		TargetChapters: chapters, ChapterMinChars: minChars,
		ChapterMaxChars: maxChars, MaxCalls: maxCalls, CreatedAt: time.Now().UTC(),
	}
}

// newEngine 组装模型、持久化和 CLI 进度输出三类依赖。
func newEngine(config llm.Config, files *store.Store, maxCalls int) (*workflow.Engine, error) {
	// 将模型客户端、文件存储和进度播报器组装成 Engine。
	// reporter 只负责给 CLI 提供可见反馈，不参与生成决策，便于测试时替换成空实现。
	// 创建并校验模型客户端。
	client, err := llm.NewClient(config)
	if err != nil {
		return nil, err
	}

	// 准备只输出阶段和摘要的 CLI 进度播报器，不打印模型原文或密钥。
	reporter := func(event workflow.Event) {
		fmt.Printf("[%s] %s\n", event.Stage, event.Message)
	}

	// 将依赖交给工作流引擎，并恢复已有 usage.jsonl 的调用计数。
	return workflow.New(client, files, maxCalls, reporter)
}

// printUsage 展示最小可用工作流和密钥环境变量。
func printUsage() {
	// 使用一段固定帮助文本而不是自动拼接 flag，保证用户第一次接触项目时
	// 先看到完整的“创建—运行—查看—导出”主流程。
	fmt.Print(`story-emerge：状态化中文中篇小说 Agent

用法：
  story-emerge new --idea-file idea.md --out novels/demo --provider deepseek
  story-emerge run --project novels/demo
  story-emerge status --project novels/demo
  story-emerge export --project novels/demo
  story-emerge serve --provider auto --addr 127.0.0.1:8787

密钥：
  DeepSeek 使用 DEEPSEEK_API_KEY
  OpenAI 使用 OPENAI_API_KEY
`)
}
