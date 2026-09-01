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

	appconfig "story_emerge/internal/config"
	"story_emerge/internal/llm"
	"story_emerge/internal/store"
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
	// Web 是产品的默认入口
	// 用户不需要先理解 serve 这个进程级命令。serve 仍作为显式别名保留，
	// 便于需要传递 --addr、--data 等运行参数时使用。
	if len(arguments) == 0 {
		return serveWeb(ctx, nil)
	}

	// 根据命令名转交给对应处理器；处理器各自负责参数校验和业务副作用。
	switch arguments[0] {
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
		return fmt.Errorf("未知命令 %q；可用命令：run、status、export、serve", arguments[0])
	}
}

// serveWeb 启动本地 Web 产品入口，并在进程信号到来时等待 HTTP 连接优雅退出。
func serveWeb(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("addr", "127.0.0.1:8787", "Web 监听地址")
	dataRoot := flags.String("data", "novels", "小说项目保存目录")
	configFile := flags.String("config", "config.yaml", "模型配置 YAML 文件")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	roleConfigs, err := loadRoleConfigs(*configFile)
	if err != nil {
		return err
	}
	runtime, err := webapp.NewWorkflowRuntime(roleConfigs)
	if err != nil {
		return err
	}
	application, err := webapp.NewServer(ctx, *dataRoot, runtime)
	if err != nil {
		return err
	}
	return listenWeb(ctx, *address, application.Handler())
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

// runNovel 从项目 HEAD 恢复状态并按 limit 继续生成章节。
func runNovel(ctx context.Context, arguments []string) error {
	// run 永远从磁盘上的 project.json 和 HEAD 恢复，不把进度依赖在进程内存里。
	// 因而进程中断后再次执行同一命令，会从最后一次提交的章节继续。

	// 解析并校验项目路径。
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	configFile := flags.String("config", "config.yaml", "模型配置 YAML 文件")
	limit := flags.Int("chapters", 0, "本次最多写几章；0 表示写到故事自然完成")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}

	// 从磁盘读取项目契约；故事进度在项目中，模型连接配置由当前 YAML 提供。
	files := store.New(*root)
	project, err := files.LoadProject()
	if err != nil {
		return err
	}

	// 读取本次运行使用的角色模型配置。
	roleConfigs, err := loadRoleConfigs(*configFile)
	if err != nil {
		return err
	}

	// 创建可恢复引擎并继续章节状态机。
	engine, err := newEngine(roleConfigs, files, project.MaxCalls)
	if err != nil {
		return err
	}

	// limit 只限制本次运行步数，不会修改故事自己的完成状态。
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

// loadRoleConfigs 读取并解析当前进程使用的全局角色模型配置。
func loadRoleConfigs(path string) (map[string]llm.Config, error) {
	settings, err := appconfig.Load(path)
	if err != nil {
		return nil, err
	}
	return settings.RoleConfigs()
}

// newEngine 组装按角色路由的模型、持久化和 CLI 进度输出三类依赖。
func newEngine(roleConfigs map[string]llm.Config, files *store.Store, maxCalls int) (*workflow.Engine, error) {
	// 将模型客户端、文件存储和进度播报器组装成 Engine。
	// reporter 只负责给 CLI 提供可见反馈，不参与生成决策，便于测试时替换成空实现。
	// 创建并校验按角色分发的模型客户端。
	client, err := llm.NewRoleClient(roleConfigs)
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

// printUsage 展示最小可用工作流和 YAML 配置入口。
func printUsage() {
	// 使用一段固定帮助文本而不是自动拼接 flag，保证用户第一次接触项目时
	// 先看到完整的“直接运行启动 Web—续写—查看—导出”主流程。
	fmt.Print(`story-emerge：状态化中文中篇小说 Agent

用法：
  story-emerge                         # 直接启动 Web
  story-emerge serve --config config.yaml --addr 127.0.0.1:8787
  story-emerge run --project novels/demo --config config.yaml
  story-emerge status --project novels/demo
  story-emerge export --project novels/demo

模型配置：
  复制 config.example.yaml 为 config.yaml，并分别配置各角色的 provider/model
`)
}
