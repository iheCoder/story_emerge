package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"story_emerge/internal/observe"
	"story_emerge/internal/store"
)

// newObserver keeps observation wiring out of workflow construction. Recording is diagnostic only:
// persistence failures are reported to stderr and never turn a valid story transition into a failed one.
func newObserver(files *store.Store) *observe.Recorder {
	return observe.NewJSONL(filepath.Join(files.Root(), "observations.jsonl"), func(err error) {
		fmt.Fprintf(os.Stderr, "Observation 写入失败 [%s]: %v\n", files.Root(), err)
	})
}
