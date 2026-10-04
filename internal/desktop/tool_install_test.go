package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func waitJobIdle(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !app.GetToolInstallJob().Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("install job timeout")
}

func TestGetToolInstallRecipeClaude(t *testing.T) {
	app := NewAppWith(Options{Providers: providers.Builtins(), Home: t.TempDir()})
	v, err := app.GetToolInstallRecipe("claude")
	if err != nil || !strings.Contains(v.InstallCmd, "claude-code") || !v.CanPurge {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestGetToolInstallRecipeGenericRejected(t *testing.T) {
	g := providers.Generic{Spec: providers.GenericSpec{ID: "codebuddy", Name: "CB"}}
	app := NewAppWith(Options{Providers: []providers.Provider{g}, Home: t.TempDir()})
	if _, err := app.GetToolInstallRecipe("codebuddy"); err == nil {
		t.Fatal("want error")
	}
}

func TestInstallBusy(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		InstallRunner: func(ctx context.Context, shell, cmdline string, onLog func(string)) error {
			close(started)
			<-block
			return nil
		},
	})
	if err := app.InstallBuiltinTool("gemini"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := app.InstallBuiltinTool("claude"); err == nil {
		t.Fatal("want busy")
	}
	close(block)
	waitJobIdle(t, app)
}

func TestUninstallCursorPurgeRejected(t *testing.T) {
	app := NewAppWith(Options{Providers: providers.Builtins(), Home: t.TempDir()})
	if err := app.UninstallBuiltinTool("cursor", true); err == nil {
		t.Fatal("want cursor purge error")
	}
}

func TestInstallSuccessRescansTools(t *testing.T) {
	var scans int
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			scans++
			return &discovery.Result{}, nil
		},
		Windows:       NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error { return nil },
	})
	if err := app.InstallBuiltinTool("gemini"); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && scans == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if scans == 0 {
		t.Fatal("成功后应 ScanSessions")
	}
}

func TestUninstallClaudeRemovesLocalBinWhenNpmFails(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".claude", "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	var bin string
	if runtime.GOOS == "windows" {
		bin = filepath.Join(local, "claude.cmd")
	} else {
		bin = filepath.Join(local, "claude")
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      home,
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return errors.New("npm fail")
		},
	})
	if err := app.UninstallBuiltinTool("claude", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	if job := app.GetToolInstallJob(); job.Error != "" {
		t.Fatalf("删掉残留二进制后任务应成功, job=%+v", job)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("应删除 ~/.claude/local 下的 claude")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); err != nil {
		t.Fatal("未勾选清除配置时不得删除 ~/.claude")
	}
	for _, tool := range app.GetTools() {
		if tool.ID == "claude" && tool.BinPath != "" {
			t.Fatalf("卸载后 BinPath 应为空, got %+v", tool)
		}
	}
}

func TestUninstallFailureDoesNotPurge(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      home,
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return errors.New("npm fail")
		},
	})
	_ = app.UninstallBuiltinTool("gemini", true)
	waitJobIdle(t, app)
	if _, err := os.Stat(cfg); err != nil {
		t.Fatal("失败不得删配置目录")
	}
}

func TestUninstallPurgeRemovesDeclaredDirs(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".gemini")
	other := filepath.Join(home, ".keep")
	_ = os.MkdirAll(cfg, 0o755)
	_ = os.MkdirAll(other, 0o755)
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      home,
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows:       NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error { return nil },
	})
	if err := app.UninstallBuiltinTool("gemini", true); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatal("应删除 ~/.gemini")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("不得删除未声明目录")
	}
}

func TestInstallUnknownTool(t *testing.T) {
	app := NewAppWith(Options{Providers: providers.Builtins(), Home: t.TempDir()})
	if err := app.InstallBuiltinTool("nope"); err == nil {
		t.Fatal("want unknown tool")
	}
}

func TestUninstallCursorDeletesBin(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "cursor-agent.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	called := false
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      home,
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			called = true
			return nil
		},
	})
	setTools(t, app, []discovery.Tool{{ID: "cursor", Installed: true, BinPath: bin}})
	if err := app.UninstallBuiltinTool("cursor", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	if called {
		t.Fatal("cursor 卸载不应走 runner")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("应删除 cursor-agent 二进制")
	}
}

func TestInstallDoneEmittedAfterIdle(t *testing.T) {
	var mu sync.Mutex
	saw := false
	runningAtDone := true
	var app *App
	app = NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return nil
		},
		Emit: func(name string, _ ...any) {
			if name != "tool:install:done" {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			saw = true
			runningAtDone = app.GetToolInstallJob().Running
		},
	})
	if err := app.InstallBuiltinTool("gemini"); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	mu.Lock()
	defer mu.Unlock()
	if !saw {
		t.Fatal("应发出 tool:install:done")
	}
	if runningAtDone {
		t.Fatal("done 事件发出时 Running 应为 false")
	}
}

func TestInstallEnsuresToolBinsOnPATH(t *testing.T) {
	isolated := t.TempDir()
	t.Setenv("PATH", isolated)
	var want []string
	if runtime.GOOS == "windows" {
		local := t.TempDir()
		appdata := t.TempDir()
		t.Setenv("LOCALAPPDATA", local)
		t.Setenv("APPDATA", appdata)
		want = []string{filepath.Join(appdata, "npm"), filepath.Join(local, "cursor-agent")}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		want = []string{filepath.Join(home, ".local", "bin")}
	}
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return nil
		},
	})
	if err := app.InstallBuiltinTool("gemini"); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	path := os.Getenv("PATH")
	for _, dir := range want {
		if countPATH(path, dir) != 1 {
			t.Fatalf("PATH 应恰好包含 %q 一次, got %q", dir, path)
		}
	}
}

func TestPurgeErrorFailsJob(t *testing.T) {
	var mu sync.Mutex
	var okVal any
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return nil
		},
		PurgeDirs: func([]string) error {
			return errors.New("access denied")
		},
		Emit: func(name string, data ...any) {
			if name != "tool:install:done" || len(data) == 0 {
				return
			}
			payload, _ := data[0].(map[string]any)
			mu.Lock()
			okVal = payload["ok"]
			mu.Unlock()
		},
	})
	if err := app.UninstallBuiltinTool("gemini", true); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	job := app.GetToolInstallJob()
	if job.Error == "" || !strings.Contains(job.Log, "access denied") {
		t.Fatalf("清除失败应记入任务, job=%+v", job)
	}
	mu.Lock()
	defer mu.Unlock()
	if okVal != false {
		t.Fatalf("done ok = %v, want false", okVal)
	}
}

func TestRunScanKeepsInstallTools(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	done := make(chan struct{})
	app := NewAppWith(Options{
		Home: t.TempDir(),
		Providers: []providers.Provider{fakeProvider{
			id: "cursor",
		}},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			close(started)
			<-block
			return &discovery.Result{Sessions: []providers.Session{{ID: "s-new"}}}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		Emit: func(name string, _ ...any) {
			if name == "scan:done" {
				close(done)
			}
		},
	})
	go app.runScan()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not start")
	}
	fresh := []discovery.Tool{{ID: "cursor", Installed: true, BinPath: filepath.Join(t.TempDir(), "cursor-agent.exe"), Source: "install-dir"}}
	app.mu.Lock()
	app.tools = fresh
	app.keepInstallTools = true
	app.mu.Unlock()
	close(block)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("应发出 scan:done")
	}
	got := app.GetTools()
	if len(got) != 1 || got[0].BinPath != fresh[0].BinPath || !got[0].Installed {
		t.Fatalf("扫描不得覆盖安装后的工具表, got %+v", got)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.result == nil || len(app.result.Sessions) != 1 {
		t.Fatal("仍应写入扫描到的会话")
	}
}

func countPATH(path, dir string) int {
	n := 0
	for _, p := range filepath.SplitList(path) {
		if runtime.GOOS == "windows" {
			if strings.EqualFold(filepath.Clean(p), filepath.Clean(dir)) {
				n++
			}
			continue
		}
		if filepath.Clean(p) == filepath.Clean(dir) {
			n++
		}
	}
	return n
}
