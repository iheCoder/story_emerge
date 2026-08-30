package webapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"story_emerge/internal/store"
)

// syncLibrary 发现磁盘上的已提交小说，并只补充当前进程尚未认识的项目。
// 正在生成的内存任务不会被扫描结果覆盖，否则会丢失实时阶段与错误信息。
func (server *Server) syncLibrary() error {
	entries, err := os.ReadDir(server.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取书架失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			server.restoreLibraryEntry(entry.Name())
		}
	}
	return nil
}

func (server *Server) restoreLibraryEntry(id string) {
	server.mu.RLock()
	_, exists := server.jobs[id]
	server.mu.RUnlock()
	if exists {
		return
	}

	restored, err := loadLibraryJob(id, filepath.Join(server.root, id))
	if err != nil {
		return
	}
	server.mu.Lock()
	if _, exists := server.jobs[id]; !exists {
		server.jobs[id] = restored
	}
	server.mu.Unlock()
}

func loadLibraryJob(id, root string) (*job, error) {
	files := store.New(root)
	project, _, state, err := loadCommittedStory(files)
	if err != nil {
		return nil, err
	}
	updatedAt := committedAt(root, project.CreatedAt)
	status, phase := restoredStatus(state.Chapter, state.StoryStatus)
	return &job{
		ID: id, Root: root, Length: normalizedLength(project.LengthProfile),
		Status: status, Phase: phase, UpdatedAt: updatedAt,
	}, nil
}

func committedAt(root string, fallback time.Time) time.Time {
	info, err := os.Stat(filepath.Join(root, "HEAD"))
	if err == nil {
		return info.ModTime().UTC()
	}
	return fallback
}

func restoredStatus(chapter int, storyStatus string) (string, string) {
	if storyStatus == "completed" {
		return "complete", "故事已经完整收束"
	}
	if chapter == 0 {
		return "ready", "故事方向已经准备好"
	}
	return "ready", "可以继续阅读"
}

func normalizedLength(profile string) string {
	if map[string]bool{"short": true, "medium": true, "long": true, "epic": true}[profile] {
		return profile
	}
	return "medium"
}
