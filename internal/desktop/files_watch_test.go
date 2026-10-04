package desktop

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileWatchEmitsDebounced(t *testing.T) {
	env := newFilesEnv(t)
	fileWatchDebounce = 30 * time.Millisecond
	t.Cleanup(func() { fileWatchDebounce = 300 * time.Millisecond })

	var mu sync.Mutex
	var events []string
	// Emit 经 snapshot() 读取，须加锁写入（对照 app_test.go）
	env.app.mu.Lock()
	env.app.opts.Emit = func(name string, data ...any) {
		if name != "files:changed" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if len(data) > 0 {
			if m, ok := data[0].(map[string]string); ok {
				events = append(events, m["path"])
			}
		}
	}
	env.app.mu.Unlock()

	if err := env.app.StartFileWatch(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.app.StopFileWatch(env.root) })

	writeFile(t, env.root, "watched.go", "package w\n")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	want := filepath.Clean(env.root)
	if len(events) < 1 || events[0] != want {
		t.Fatalf("期望 files:changed path=%s, got %v", want, events)
	}
}

// TestFileWatchStopPreventsEmit：Stop 后不得再 Emit（含 close 后迟到的 schedule）。
func TestFileWatchStopPreventsEmit(t *testing.T) {
	env := newFilesEnv(t)
	fileWatchDebounce = 30 * time.Millisecond
	t.Cleanup(func() { fileWatchDebounce = 300 * time.Millisecond })

	var mu sync.Mutex
	var events []string
	env.app.mu.Lock()
	env.app.opts.Emit = func(name string, data ...any) {
		if name != "files:changed" {
			return
		}
		mu.Lock()
		events = append(events, "hit")
		mu.Unlock()
	}
	env.app.mu.Unlock()

	if err := env.app.StartFileWatch(env.root); err != nil {
		t.Fatal(err)
	}
	cleaned := filepath.Clean(env.root)
	env.app.watchMu.Lock()
	fw := env.app.watches[cleaned]
	env.app.watchMu.Unlock()
	if fw == nil {
		t.Fatal("期望 Start 后存在 watcher")
	}

	env.app.StopFileWatch(env.root)

	// 模拟 close 后仍处理已出队 Events 的竞态
	env.app.scheduleFileWatchNotify(fw)
	writeFile(t, env.root, "after-stop.go", "package a\n")

	time.Sleep(fileWatchDebounce + 200*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 0 {
		t.Fatalf("Stop 后不应收到 files:changed, got %d", len(events))
	}
}
