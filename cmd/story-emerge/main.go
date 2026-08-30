package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"story_emerge/internal/llm"
	"story_emerge/internal/store"
	"story_emerge/internal/story"
	"story_emerge/internal/workflow"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		printUsage()
		return nil
	}
	switch arguments[0] {
	case "new":
		return runNew(ctx, arguments[1:])
	case "run":
		return runNovel(ctx, arguments[1:])
	case "status":
		return showStatus(arguments[1:])
	case "export":
		return exportNovel(arguments[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("未知命令 %q；可用命令：new、run、status、export", arguments[0])
	}
}

func runNew(ctx context.Context, arguments []string) error {
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
	idea, root, err := readNewInputs(*ideaFile, *output)
	if err != nil {
		return err
	}
	config, err := llm.ConfigFromEnv(*provider, *model)
	if err != nil {
		return err
	}
	project := newProject(*name, root, idea, config, *chapters, *minChars, *maxChars, *maxCalls)
	engine, err := newEngine(config, store.New(root), project.MaxCalls)
	if err != nil {
		return err
	}
	_, err = engine.Initialize(ctx, project)
	return err
}

func runNovel(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	limit := flags.Int("chapters", 0, "本次最多写几章；0 表示写到项目目标")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}

	files := store.New(*root)
	project, err := files.LoadProject()
	if err != nil {
		return err
	}

	config, err := llm.ConfigFromEnv(project.Provider, project.Model)
	if err != nil {
		return err
	}

	engine, err := newEngine(config, files, project.MaxCalls)
	if err != nil {
		return err
	}

	return engine.Run(ctx, *limit)
}

func showStatus(arguments []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}
	content, err := os.ReadFile(filepath.Join(*root, "status.md"))
	if err != nil {
		return err
	}
	fmt.Print(string(content))
	return nil
}

func exportNovel(arguments []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	root := flags.String("project", "", "小说项目目录")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("必须提供 --project")
	}
	path, err := store.New(*root).ExportManuscript()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func readNewInputs(ideaFile, output string) (string, string, error) {
	if strings.TrimSpace(ideaFile) == "" || strings.TrimSpace(output) == "" {
		return "", "", fmt.Errorf("new 必须提供 --idea-file 和 --out")
	}
	content, err := os.ReadFile(ideaFile)
	if err != nil {
		return "", "", fmt.Errorf("读取创作点子失败: %w", err)
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(content)), absolute, nil
}

func newProject(
	name, root, idea string,
	config llm.Config,
	chapters, minChars, maxChars, maxCalls int,
) story.Project {
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(root)
	}
	return story.Project{
		Version: story.FormatVersion, Name: name, Idea: idea,
		Provider: config.Provider, Model: config.Model,
		TargetChapters: chapters, ChapterMinChars: minChars,
		ChapterMaxChars: maxChars, MaxCalls: maxCalls, CreatedAt: time.Now().UTC(),
	}
}

func newEngine(config llm.Config, files *store.Store, maxCalls int) (*workflow.Engine, error) {
	client, err := llm.NewClient(config)
	if err != nil {
		return nil, err
	}
	reporter := func(event workflow.Event) {
		fmt.Printf("[%s] %s\n", event.Stage, event.Message)
	}
	return workflow.New(client, files, maxCalls, reporter)
}

func printUsage() {
	fmt.Print(`story-emerge：状态化中文中篇小说 Agent

用法：
  story-emerge new --idea-file idea.md --out novels/demo --provider deepseek
  story-emerge run --project novels/demo
  story-emerge status --project novels/demo
  story-emerge export --project novels/demo

密钥：
  DeepSeek 使用 DEEPSEEK_API_KEY
  OpenAI 使用 OPENAI_API_KEY
`)
}
