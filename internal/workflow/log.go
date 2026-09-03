package workflow

import (
	"fmt"
	"log/slog"
	"os"
)

// log 在 CLI 和 Web 共用的引擎层记录真实阶段，避免日志只存在于某个页面或终端。
// 日志写入失败不能把已提交章节改判为失败；明确报告到 stderr，供启动终端排查磁盘问题。
func (engine *Engine) log(level slog.Level, stage, message string, attrs ...slog.Attr) {
	if err := engine.store.AppendLog(level, stage, message, attrs...); err != nil {
		fmt.Fprintf(os.Stderr, "运行日志写入失败 [%s] %s: %v；原事件: %s %v\n", engine.store.Root(), stage, err, message, attrs)
	}
}

// logOutcome 覆盖网络调用之外的失败：字段校验、预算耗尽、上下文取消和文件提交错误。
// 由入口的 defer 读取最终返回值，不把“模型请求成功”误记为“本轮生成成功”。
func (engine *Engine) logOutcome(stage string, err error) {
	if err != nil {
		engine.log(slog.LevelError, stage, "运行失败", slog.String("error", err.Error()))
		return
	}
	engine.log(slog.LevelInfo, stage, "本轮运行结束")
}
