package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"story_emerge/internal/story"
)

func TestDirectionReviewPreservesSpineAcrossCheckpointFailure(t *testing.T) {
	// 场景：Director 调整当前位置，审计文件成功，但 checkpoint 目录暂时不可写。
	// 预期：失败后旧方向仍生效；重试仅更新方向与复查元数据，固定 Spine 始终保留。
	files := preparedStore(t)
	commit := acceptedCommit()
	commit.Review.StoryComplete = false
	chapter := "# 第1章 选择\n\n他决定和朋友一起面对后果。"
	before, err := files.CommitChapter(chapter, commit)
	if err != nil {
		t.Fatal(err)
	}
	updated := before.Direction
	updated.CurrentPosition = "合作意愿已形成，实际共同承担尚待展开"
	review := story.DirectionReview{AfterChapter: 1, Version: 2, Decision: story.DirectorDecision{
		Action: story.DirectorAdjust, Direction: updated, Reason: "第1章主动形成了合作意愿",
	}}

	// 只禁止替换 checkpoint；旧文件保持可读，让故障发生在审计写入之后。
	directory := files.path("checkpoints")
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0755) })
	if _, err := files.CommitDirectionReview(review); err == nil {
		t.Fatal("不可写 checkpoint 却完成了规划提交")
	}
	if _, err := os.Stat(files.path(directionReviewPath(1))); err != nil {
		t.Fatalf("故障未发生在审计写入之后: %v", err)
	}
	reopened := New(files.Root())
	after, err := reopened.LoadState()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("先行审计污染正式状态: %#v %v", after, err)
	}

	// 清除故障，用新 Store 重试同一章的规划提交；正文、HEAD 与事实始终不动。
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.CommitDirectionReview(review); err != nil {
		t.Fatal(err)
	}
	after, err = New(files.Root()).LoadState()
	expected := before
	expected.Direction = updated
	expected.DirectionVersion, expected.DirectionReviewedAfterChapter = 2, 1
	if err != nil || !reflect.DeepEqual(after, expected) {
		t.Fatalf("重试没有整体更新规划: %#v %v", after, err)
	}
	saved, err := reopened.LoadChapter(1)
	if err != nil || saved != chapter {
		t.Fatalf("规划修正改写了正文: %q %v", saved, err)
	}
}

func preparedStore(t *testing.T) *Store {
	t.Helper()
	files := New(filepath.Join(t.TempDir(), "novel"))
	// 带空白的原始授权必须原样保留；存储层不能替用户清洗创作输入。
	project := story.Project{Idea: "  用户原始想法\n\n", LengthProfile: "medium", MaxCalls: 100}
	if err := files.Prepare(project); err != nil {
		t.Fatal(err)
	}
	core := story.StoryCore{StoryEngine: story.StoryEngine{Loop: "行动产生局面", ProgressionAxis: "逐渐承担责任"}, ReaderPromises: []story.ReaderPromise{{Promise: "承诺一", PayoffShape: "经历"}, {Promise: "承诺二", PayoffShape: "选择"}, {Promise: "承诺三", PayoffShape: "结果"}}, ExperienceContract: story.ExperienceContract{TargetExperience: "真实生活"}}
	if err := files.CommitGenesis(story.Genesis{Title: "测试书", StoryCore: core, StorySpine: []story.SpineStage{{From: "临时相处", To: "主动信任", WhyItMatters: "共同选择需要信任", ExitEvidence: "有事时愿意主动寻求对方支持"}}, CurrentDirection: story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "共同生活", DesiredShift: "逐渐信任", ReaderExpectation: "读者等待信任如何形成"}}); err != nil {
		t.Fatal(err)
	}
	return files
}
func acceptedCommit() story.ChapterCommit {
	return story.ChapterCommit{Chapter: 1, Title: "选择", Review: story.EditorDecision{ChapterDecision: story.EditorAccept, Assessment: story.EditorAssessment{Contribution: "开始承担责任", Sequence: "形成落点", Execution: "结果成立"}, StoryComplete: true}, Result: story.CommitResult{ChapterSummary: "人物接受结果", TrajectoryEntry: story.TrajectoryMove{StoryMove: "开始承担责任", NarrativeShape: "选择 → 接受结果"}}}
}

func TestCommitFailureKeepsOldHEADAndHidesOrphanArtifacts(t *testing.T) {
	// 场景：正文与提交记录已写完，但检查点目标被一个目录占用，模拟最终写入失败。
	// 预期：章节、方向、字数、完结标记都不可见；消除故障后可从同一 HEAD 重试。
	files := preparedStore(t)
	obstruction := files.path(checkpointPath(1))
	if err := os.Mkdir(obstruction, 0755); err != nil {
		t.Fatal(err)
	}
	chapter := "# 第1章 选择\n\n他接受了结果。"
	if _, err := files.CommitChapter(chapter, acceptedCommit()); err == nil {
		t.Fatal("故障提交意外成功")
	}
	state, err := files.LoadState()
	if err != nil || state.Chapter != 0 || state.Completed || state.WrittenCharacters != 0 || state.Direction.Focus != "共同生活" {
		t.Fatalf("失败推进了状态: %#v %v", state, err)
	}
	if _, err := files.LoadChapter(1); err == nil {
		t.Fatal("孤儿正文越过 HEAD 可见")
	}
	ledger, err := files.LoadLedger()
	if err != nil || len(ledger) != 0 {
		t.Fatalf("总账混入未提交章节: %#v %v", ledger, err)
	}
	exported, err := files.ExportManuscript()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(exported)
	if strings.Contains(string(text), "他接受了结果") {
		t.Fatal("导出混入孤儿正文")
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	next, err := files.CommitChapter(chapter, acceptedCommit())
	if err != nil || next.Chapter != 1 || !next.Completed || next.Direction.Focus != "共同生活" {
		t.Fatalf("重试未完整提交: %#v %v", next, err)
	}
}

func TestCoreAndUserIdeaStayStableAndCompletedBookCannotAdvance(t *testing.T) {
	// 场景：完成第一章后再次尝试初始化或续写。
	// 预期：Core 和原始授权不被改写，完结只来自正式验收，已有章节不可覆盖。
	files := preparedStore(t)
	before, _ := os.ReadFile(files.path("story-core.json"))
	project, err := files.LoadProject()
	if err != nil || project.Idea != "  用户原始想法\n\n" {
		t.Fatalf("原始授权被改写: %q %v", project.Idea, err)
	}
	if _, err := files.CommitChapter("# 第1章 选择\n\n他接受了结果。", acceptedCommit()); err != nil {
		t.Fatal(err)
	}
	if _, err := files.CommitChapter("# 第1章 错误\n\n改写。", acceptedCommit()); err == nil {
		t.Fatal("已提交章节被覆盖")
	}
	if err := files.CommitGenesis(story.Genesis{}); err == nil {
		t.Fatal("Core 可以被重新初始化")
	}
	after, _ := os.ReadFile(files.path("story-core.json"))
	if string(before) != string(after) {
		t.Fatal("作品核心被修改")
	}
}

func TestDirectionReviewUpdatesCurrentCheckpointWithoutAdvancingHEAD(t *testing.T) {
	// 场景：第一章已经正式提交且尚未完结，Director 随后调整阶段 Direction。
	// 预期：同一份 checkpoint 得到新方向和审计元数据，HEAD、正文与章节提交记录均不改变。
	files := preparedStore(t)
	commit := acceptedCommit()
	commit.Review.StoryComplete = false
	chapter := "# 第1章 选择\n\n他接受了结果。"
	if _, err := files.CommitChapter(chapter, commit); err != nil {
		t.Fatal(err)
	}
	beforeCommit, err := os.ReadFile(files.path(commitPath(1)))
	if err != nil {
		t.Fatal(err)
	}

	updated := story.Direction{CurrentPosition: "当前关系仍在形成，稳定信任尚未建立", Focus: "面对后果", DesiredShift: "共同承担选择", ReaderExpectation: "读者等待选择如何改变两人的关系"}
	next, err := files.CommitDirectionReview(story.DirectionReview{
		AfterChapter: 1, Version: 2,
		Decision: story.DirectorDecision{Action: story.DirectorAdjust, Direction: updated, Reason: "初步选择已经形成"},
	})
	if err != nil {
		t.Fatal(err)
	}
	head, _ := os.ReadFile(files.path("HEAD"))
	afterCommit, _ := os.ReadFile(files.path(commitPath(1)))
	savedChapter, _ := files.LoadChapter(1)
	if string(head) != "001\n" || savedChapter != chapter || string(beforeCommit) != string(afterCommit) {
		t.Fatal("Direction Review 改写了章节提交边界")
	}
	if next.Direction != updated || next.DirectionVersion != 2 || next.DirectionReviewedAfterChapter != 1 {
		t.Fatalf("Direction Review 没有更新当前检查点: %#v", next)
	}
	if _, err := os.Stat(files.path(directionReviewPath(1))); err != nil {
		t.Fatalf("缺少 Direction Review 审计记录: %v", err)
	}
}
