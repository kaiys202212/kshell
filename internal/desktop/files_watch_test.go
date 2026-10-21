package desktop

import (
	"os"
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

// TestFileWatchSkipsExcludedDir：node_modules 等排除目录不监视，写其内文件不得 files:changed。
func TestFileWatchSkipsExcludedDir(t *testing.T) {
	env := newFilesEnv(t)
	fileWatchDebounce = 30 * time.Millisecond
	gitWatchDebounce = 30 * time.Millisecond
	t.Cleanup(func() {
		fileWatchDebounce = 300 * time.Millisecond
		gitWatchDebounce = 300 * time.Millisecond
	})

	var mu sync.Mutex
	var fileHits int
	env.app.mu.Lock()
	env.app.opts.Emit = func(name string, data ...any) {
		if name != "files:changed" {
			return
		}
		mu.Lock()
		fileHits++
		mu.Unlock()
	}
	env.app.mu.Unlock()

	if err := env.app.StartFileWatch(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.app.StopFileWatch(env.root) })

	// 丢弃 Start 后可能迟到的初始事件，再测排除目录
	time.Sleep(fileWatchDebounce + 100*time.Millisecond)
	mu.Lock()
	fileHits = 0
	mu.Unlock()

	cleaned := filepath.Clean(env.root)
	env.app.watchMu.Lock()
	fw := env.app.watches[cleaned]
	env.app.watchMu.Unlock()
	if fw == nil {
		t.Fatal("期望 watcher")
	}
	storm := filepath.Join(env.root, "node_modules", "storm.js")
	if !pathUnderWatchSkip(storm, fw.root, fw.skip) {
		t.Fatalf("storm 路径应按 skip 过滤: root=%q skip=%v", fw.root, fw.skip)
	}
	if !pathUnderWatchSkip(filepath.Join(env.root, "node_modules"), fw.root, fw.skip) {
		t.Fatal("node_modules 目录应按 skip 过滤")
	}

	writeFile(t, env.root, filepath.Join("node_modules", "storm.js"), "noise\n")
	time.Sleep(fileWatchDebounce + 150*time.Millisecond)
	mu.Lock()
	got := fileHits
	mu.Unlock()
	if got != 0 {
		t.Fatalf("排除目录内写入不应 files:changed, got %d", got)
	}
}

// TestFileWatchDeepCreateThenWrite：新建深目录后写文件应能收到 files:changed（递归补 watch）。
func TestFileWatchDeepCreateThenWrite(t *testing.T) {
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

	deep := filepath.Join(env.root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// 等目录 Create 事件被处理并补 watch
	time.Sleep(80 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(deep, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if len(events) < 1 {
		t.Fatal("深目录写入后期望 files:changed")
	}
}

// TestFileWatchErrorForcesNotify：Errors（如溢出）应防抖后仍 Emit files:changed。
func TestFileWatchErrorForcesNotify(t *testing.T) {
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

	cleaned := filepath.Clean(env.root)
	env.app.watchMu.Lock()
	fw := env.app.watches[cleaned]
	env.app.watchMu.Unlock()
	if fw == nil {
		t.Fatal("期望 watcher")
	}
	// 不依赖真实溢出：直接走与 Errors 分支相同的 schedule
	env.app.scheduleFileWatchNotify(fw)

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
		t.Fatalf("期望溢出/错误后 files:changed path=%s, got %v", want, events)
	}
}

// TestGitMetaWatchEmitsGitChanged：改 HEAD/index 发 git:changed，不发 files:changed；objects 噪声忽略。
func TestGitMetaWatchEmitsGitChanged(t *testing.T) {
	env := newFilesEnv(t)
	fileWatchDebounce = 30 * time.Millisecond
	gitWatchDebounce = 30 * time.Millisecond
	t.Cleanup(func() {
		fileWatchDebounce = 300 * time.Millisecond
		gitWatchDebounce = 300 * time.Millisecond
	})

	gitDir := filepath.Join(env.root, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "objects", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("DIRC"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var gitHits, fileHits int
	env.app.mu.Lock()
	env.app.opts.Emit = func(name string, data ...any) {
		mu.Lock()
		defer mu.Unlock()
		switch name {
		case "git:changed":
			gitHits++
		case "files:changed":
			fileHits++
		}
	}
	env.app.mu.Unlock()

	if err := env.app.StartFileWatch(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.app.StopFileWatch(env.root) })

	// 丢弃挂表时可能冒出的目录事件，再测纯 git 元数据
	time.Sleep(fileWatchDebounce + 100*time.Millisecond)
	mu.Lock()
	gitHits, fileHits = 0, 0
	mu.Unlock()

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		g := gitHits
		mu.Unlock()
		if g >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if gitHits < 1 {
		mu.Unlock()
		t.Fatal("改 HEAD 期望 git:changed")
	}
	if fileHits != 0 {
		mu.Unlock()
		t.Fatalf("HEAD 变更不应 files:changed, got %d", fileHits)
	}
	gitHits, fileHits = 0, 0
	mu.Unlock()

	if err := os.WriteFile(filepath.Join(gitDir, "objects", "ab", "cd"), []byte("blob"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(gitWatchDebounce + 150*time.Millisecond)
	mu.Lock()
	if gitHits != 0 || fileHits != 0 {
		t.Fatalf("objects 噪声不应 Emit, git=%d file=%d", gitHits, fileHits)
	}
	mu.Unlock()
}
