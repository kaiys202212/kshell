package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
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

// isolatePATH 避免卸载走 LookPath 时删掉本机真实 CLI。
func isolatePATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
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
	// scans 由异步 ScanSessions goroutine 与测试主 goroutine 同时访问，
	// 用原子计数避免 -race 报竞争。
	var scans atomic.Int32
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			scans.Add(1)
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
	for time.Now().Before(deadline) && scans.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if scans.Load() == 0 {
		t.Fatal("成功后应 ScanSessions")
	}
}

func TestUninstallClaudeRemovesLocalBinWhenNpmFails(t *testing.T) {
	isolatePATH(t)
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
	if job := app.GetToolInstallJob(); !strings.Contains(job.Log, "npm fail") {
		t.Fatalf("应保留命令失败原因, log=%q", app.GetToolInstallJob().Log)
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

func TestUninstallClaudeRemovesNpmGlobalOffPATH(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 才会把 %APPDATA%\\npm 补进 PATH")
	}
	isolatePATH(t)
	home := t.TempDir()
	appdata := t.TempDir()
	t.Setenv("APPDATA", appdata)
	npmDir := filepath.Join(appdata, "npm")
	if err := os.MkdirAll(npmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(npmDir, "claude.cmd")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("应删掉 npm 全局残留, job=%+v", job)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("应删除 %APPDATA%\\npm\\claude.cmd")
	}
}

func TestUninstallFailureDoesNotPurge(t *testing.T) {
	isolatePATH(t)
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
	isolatePATH(t)
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
	isolatePATH(t)
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
	isolatePATH(t)
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

// node 入口形态（BinArgs 非空）：卸载应删除 node.exe 所在的整个版本目录，
// 只删 node.exe 会留下 index.js 与目录，DetectAll 重扫后工具依然「已安装」。
func TestUninstallCursorNodeEntryRemovesVersionDir(t *testing.T) {
	isolatePATH(t)
	appdata := t.TempDir()
	t.Setenv("LOCALAPPDATA", appdata)
	vdir := filepath.Join(appdata, "cursor-agent", "versions", "2026.10.01-test")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"node.exe", "index.js"} {
		if err := os.WriteFile(filepath.Join(vdir, f), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
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
	setTools(t, app, []discovery.Tool{{
		ID: "cursor", Installed: true,
		BinPath: filepath.Join(vdir, "node.exe"),
		BinArgs: []string{filepath.Join(vdir, "index.js")},
	}})
	if err := app.UninstallBuiltinTool("cursor", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	if job := app.GetToolInstallJob(); job.Error != "" {
		t.Fatalf("卸载应成功, job=%+v", job)
	}
	if _, err := os.Stat(vdir); !os.IsNotExist(err) {
		t.Fatal("node 入口形态应删除整个版本目录")
	}
}

// 其他版本目录仍完整可用时，卸载必须把它们一并清掉，不能假成功。
func TestUninstallCursorNodeEntryRemovesLeftoverVersions(t *testing.T) {
	isolatePATH(t)
	appdata := t.TempDir()
	t.Setenv("LOCALAPPDATA", appdata)
	root := filepath.Join(appdata, "cursor-agent", "versions")
	mkVersion := func(name string) string {
		v := filepath.Join(root, name)
		if err := os.MkdirAll(v, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"node.exe", "index.js"} {
			if err := os.WriteFile(filepath.Join(v, f), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return v
	}
	target := mkVersion("2026.10.01-a")
	mkVersion("2026.10.02-b")
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
	setTools(t, app, []discovery.Tool{{
		ID: "cursor", Installed: true,
		BinPath: filepath.Join(target, "node.exe"),
		BinArgs: []string{filepath.Join(target, "index.js")},
	}})
	if err := app.UninstallBuiltinTool("cursor", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	if job := app.GetToolInstallJob(); job.Error != "" {
		t.Fatalf("残留版本目录应被一并清除, job=%+v", job)
	}
	for _, name := range []string{"2026.10.01-a", "2026.10.02-b"} {
		if _, err := os.Stat(filepath.Join(root, name, "node.exe")); !os.IsNotExist(err) {
			t.Fatalf("版本目录 %s 的 node.exe 应被删除", name)
		}
	}
}

// stubUninstallProvider：带安装配方的桩 provider，DetectSpec 的 InstallDirs 指向
// 测试临时目录。卸载相关测试绝不能用内置 trae（其 InstallDirs 指向本机真实
// 安装目录 %LOCALAPPDATA%\Programs\TraeCLI\bin，会误删用户安装）。
type stubUninstallProvider struct {
	fakeProvider
	recipe providers.InstallRecipe
	spec   providers.DetectSpec
}

func (p stubUninstallProvider) DetectSpec(string) providers.DetectSpec { return p.spec }
func (p stubUninstallProvider) InstallRecipe() providers.InstallRecipe  { return p.recipe }

// TestUninstallClosesToolSessionsFirst 卸载前置关闭该工具的 kshell 管理会话
//（2026-10-23 用户反馈：卸载 TRAE 报「仍检测到可执行文件」，实为 traex.exe
// 仍在运行锁文件）。其他工具的终端不受影响，关闭动作记入日志。
func TestUninstallClosesToolSessionsFirst(t *testing.T) {
	isolatePATH(t)
	home := t.TempDir()
	bindir := t.TempDir()
	bin := filepath.Join(bindir, "stubtool.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := stubUninstallProvider{
		fakeProvider: fakeProvider{id: "stubtool"},
		spec:         providers.DetectSpec{BinName: "stubtool", InstallDirs: []string{bindir}},
	}
	app := NewAppWith(Options{
		Providers: []providers.Provider{p},
		Home:      home,
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return nil
		},
	})
	setTools(t, app, []discovery.Tool{{ID: "stubtool", Installed: true, BinPath: bin}})

	backend := &stubTermBackend{}
	tm := newTerminalManagerWith(func(string, ...any) {}, backend)
	app.mu.Lock()
	app.opts.Terminals = tm
	app.mu.Unlock()

	// stubtool 的登录终端 + claude 的终端：卸载 stubtool 只应关前者。
	if _, err := tm.Open("login:stubtool", terminal.Info{
		Kind: terminal.KindLogin, ToolID: "stubtool", Title: "stub login",
	}, terminal.Spec{}, 80, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.Open("term:claude", terminal.Info{
		Kind: terminal.KindSession, ToolID: "claude", Title: "claude term",
	}, terminal.Spec{}, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := app.UninstallBuiltinTool("stubtool", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)

	job := app.GetToolInstallJob()
	if job.Error != "" {
		t.Fatalf("卸载应成功, job=%+v", job)
	}
	if !strings.Contains(job.Log, "tool.log.sessions_closed|1") {
		t.Fatalf("应记录关闭会话数, log=%q", job.Log)
	}
	backend.mu.Lock()
	stubHandle, claudeHandle := backend.handles[0], backend.handles[1]
	backend.mu.Unlock()
	if !stubHandle.isClosed() {
		t.Fatal("被卸载工具的终端应被关闭")
	}
	if claudeHandle.isClosed() {
		t.Fatal("其他工具的终端不应被关闭")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("可执行文件应被删除")
	}
}

// TestUninstallBinInUseReportsActionableError 残留删除失败（典型：进程占用）时，
// 错误换成可操作的 err.tool.bin_in_use（提示退出会话/结束进程后重试），
// 而不是只有路径的「仍检测到可执行文件」。用非空目录模拟删不掉的路径：
// os.Remove 对非空目录必败，跨平台稳定。
func TestUninstallBinInUseReportsActionableError(t *testing.T) {
	isolatePATH(t)
	home := t.TempDir()
	locked := filepath.Join(home, "locked-bin")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := stubUninstallProvider{
		fakeProvider: fakeProvider{id: "stubtool"},
		spec:         providers.DetectSpec{BinName: "stubtool", InstallDirs: []string{t.TempDir()}},
	}
	app := NewAppWith(Options{
		Providers: []providers.Provider{p},
		Home:      home,
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		InstallRunner: func(context.Context, string, string, func(string)) error {
			return nil
		},
	})
	// BinPath 指向非空目录：卸载主删与残留重删都必然失败。
	setTools(t, app, []discovery.Tool{{ID: "stubtool", Installed: true, BinPath: locked}})
	if err := app.UninstallBuiltinTool("stubtool", false); err != nil {
		t.Fatal(err)
	}
	waitJobIdle(t, app)
	job := app.GetToolInstallJob()
	if !strings.HasPrefix(job.Error, "err.tool.bin_in_use|") {
		t.Fatalf("删除失败应报 bin_in_use, got %q", job.Error)
	}
	if !strings.Contains(job.Error, locked) {
		t.Fatalf("错误应带残留路径, got %q", job.Error)
	}
}

// TestCloseToolSessionsNilManagersSafe 未装配 manager（未走 Startup）时不 panic，
// 静默返回 0：卸载入口对未初始化的应用实例同样可用。
func TestCloseToolSessionsNilManagersSafe(t *testing.T) {
	app := NewAppWith(Options{
		Providers: providers.Builtins(),
		Home:      t.TempDir(),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
	})
	var logged []string
	if n := app.closeToolSessions("trae", func(line string) { logged = append(logged, line) }); n != 0 {
		t.Fatalf("无 manager 时应关闭 0 个, got %d", n)
	}
	if len(logged) != 0 {
		t.Fatalf("无关闭不应记日志, got %v", logged)
	}
}