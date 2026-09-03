package store

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// AppendLog 把运行事件追加到小说自己的 runtime.log。它独立于 HEAD 和用量账本，
// 因此失败尝试也能留下证据；重新打开项目只会追加，不会覆盖上次运行记录。
// 调用方只传阶段、结果和诊断信息，不传提示词、小说正文或连接配置。
func (store *Store) AppendLog(level slog.Level, stage, message string, attrs ...slog.Attr) error {
	// 不创建项目目录：Initialize 必须先通过 Prepare 的空目录检查，
	// 日志不能提前占用目录，也不能让一个不存在的项目变成可恢复状态。
	file, err := os.OpenFile(store.path("runtime.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	record := slog.NewRecord(time.Now().UTC(), level, message, 0)
	record.AddAttrs(slog.String("stage", stage))
	record.AddAttrs(attrs...)
	err = slog.NewJSONHandler(file, nil).Handle(context.Background(), record)
	// 每条写完同步并关闭，没有依赖进程退出才刷新的内存缓冲。
	return errors.Join(err, file.Sync(), file.Close())
}
