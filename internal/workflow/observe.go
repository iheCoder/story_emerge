package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"story_emerge/internal/observe"
	"story_emerge/internal/store"
)

// newObserver 把 Observation 后端装配收敛在 workflow 边缘，不让业务方法感知 JSONL 或未来的 OTel。
// Observation 只是诊断旁路：写入失败明确报告到 stderr，但绝不能把有效的故事状态迁移改判为失败。
func newObserver(files *store.Store) *observe.Recorder {
	return observe.NewJSONL(filepath.Join(files.Root(), "observations.jsonl"), func(err error) {
		fmt.Fprintf(os.Stderr, "Observation 写入失败 [%s]: %v\n", files.Root(), err)
	})
}
