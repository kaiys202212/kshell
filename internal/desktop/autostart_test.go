package desktop

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// autostartRecorder 记录注入桩的调用次数与参数，测试结束恢复原实现。
type autostartRecorder struct {
	enableCalls  int
	disableCalls int
	enabledCalls int
	enablePath   string
	enableErr    error
	enabledVal   bool
}

func (r *autostartRecorder) inject(t *testing.T) {
	t.Helper()
	origEnabledFn, origSupportedFn, origEnableFn, origDisableFn := isReleaseFn, autostartSupportedFn, autostartEnableFn, autostartDisableFn
	origEnabledStateFn := autostartEnabledFn
	autostartEnableFn = func(exe string) error {
		r.enableCalls++
		r.enablePath = exe
		return r.enableErr
	}
	autostartDisableFn = func() error {
		r.disableCalls++
		return nil
	}
	autostartEnabledFn = func() (bool, error) {
		r.enabledCalls++
		return r.enabledVal, nil
	}
	t.Cleanup(func() {
		isReleaseFn, autostartSupportedFn, autostartEnableFn, autostartDisableFn = origEnabledFn, origSupportedFn, origEnableFn, origDisableFn
		autostartEnabledFn = origEnabledStateFn
	})
}

// withAutostartEnv 设定发布态与平台支持态并保证恢复。
func withAutostartEnv(t *testing.T, release, supported bool) {
	t.Helper()
	origRelease, origSupported := isReleaseFn, autostartSupportedFn
	isReleaseFn = func() bool { return release }
	autostartSupportedFn = func() bool { return supported }
	t.Cleanup(func() {
		isReleaseFn, autostartSupportedFn = origRelease, origSupported
	})
}

func newAutostartTestApp(t *testing.T, cfg config.Config) *App {
	t.Helper()
	dir := t.TempDir()
	return NewAppWith(Options{
		Config: cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"), // EnsureRoot 建的是 cache 目录
		},
	})
}

func TestAutostartInfoUnsupported(t *testing.T) {
	withAutostartEnv(t, true, false)
	r := &autostartRecorder{}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{Autostart: true})

	got := a.AutostartInfo()
	if got.Supported || got.Enabled {
		t.Fatalf("不支持平台应整体隐藏: %+v", got)
	}
	if r.enabledCalls != 0 {
		t.Fatalf("不支持时不得查注册表，Enabled 被调 %d 次", r.enabledCalls)
	}
}

func TestAutostartInfoDevHidden(t *testing.T) {
	withAutostartEnv(t, false, true) // 平台支持但 dev 构建
	r := &autostartRecorder{}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{Autostart: true})

	got := a.AutostartInfo()
	if got.Supported || got.Enabled {
		t.Fatalf("dev 构建应视为不支持: %+v", got)
	}
	if r.enabledCalls != 0 {
		t.Fatalf("dev 下不得查注册表，Enabled 被调 %d 次", r.enabledCalls)
	}
}

func TestAutostartInfoSupportedQueriesRegistry(t *testing.T) {
	withAutostartEnv(t, true, true)
	r := &autostartRecorder{enabledVal: true}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{})

	got := a.AutostartInfo()
	if !got.Supported || !got.Enabled {
		t.Fatalf("release + 注册表有值应为 {true,true}: %+v", got)
	}
	if r.enabledCalls != 1 {
		t.Fatalf("支持时应查注册表，Enabled 被调 %d 次", r.enabledCalls)
	}
}

func TestSetAutostartUnsupportedErrors(t *testing.T) {
	withAutostartEnv(t, false, true) // dev 构建整体视为不支持
	r := &autostartRecorder{}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{})

	if err := a.SetAutostart(true); err == nil || !strings.Contains(err.Error(), "err.desktop.autostart_unsupported") {
		t.Fatalf("不支持时应返回 wire 错误, got %v", err)
	}
	if r.enableCalls != 0 || r.disableCalls != 0 {
		t.Fatalf("不支持时不得动注册表: enable=%d disable=%d", r.enableCalls, r.disableCalls)
	}
}

func TestSetAutostartOn(t *testing.T) {
	withAutostartEnv(t, true, true)
	r := &autostartRecorder{}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{})

	if err := a.SetAutostart(true); err != nil {
		t.Fatalf("SetAutostart(true): %v", err)
	}
	if r.enableCalls != 1 || r.enablePath == "" {
		t.Fatalf("开应调 Enable 并带 exe 路径: calls=%d path=%q", r.enableCalls, r.enablePath)
	}
	if r.disableCalls != 0 {
		t.Fatalf("开时不得调 Disable: %d", r.disableCalls)
	}
	// config 落盘后内存与磁盘一致。
	a.mu.Lock()
	on := a.opts.Config.Autostart
	a.mu.Unlock()
	if !on {
		t.Fatal("内存 config.Autostart 应为 true")
	}
	loaded, err := config.Load(a.opts.Layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Autostart {
		t.Fatal("落盘的 config.yaml 里 autostart 应为 true")
	}
}

func TestSetAutostartOff(t *testing.T) {
	withAutostartEnv(t, true, true)
	r := &autostartRecorder{}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{Autostart: true})

	if err := a.SetAutostart(false); err != nil {
		t.Fatalf("SetAutostart(false): %v", err)
	}
	if r.disableCalls != 1 || r.enableCalls != 0 {
		t.Fatalf("关应只调 Disable: enable=%d disable=%d", r.enableCalls, r.disableCalls)
	}
	a.mu.Lock()
	on := a.opts.Config.Autostart
	a.mu.Unlock()
	if on {
		t.Fatal("内存 config.Autostart 应为 false")
	}
}

func TestSetAutostartRegistryFailureNoConfigWrite(t *testing.T) {
	withAutostartEnv(t, true, true)
	r := &autostartRecorder{enableErr: errors.New("注册表拒绝")}
	r.inject(t)
	a := newAutostartTestApp(t, config.Config{})

	if err := a.SetAutostart(true); err == nil || !strings.Contains(err.Error(), "err.desktop.autostart_failed") {
		t.Fatalf("注册表失败应返回 wire 错误, got %v", err)
	}
	// 先注册表后 config：注册表失败不得写半态。
	loaded, err := config.Load(a.opts.Layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Autostart {
		t.Fatal("注册表失败时不得落盘 config.Autostart=true")
	}
}

func TestSyncAutostartOnStartup(t *testing.T) {
	t.Run("开→Enable", func(t *testing.T) {
		withAutostartEnv(t, true, true)
		r := &autostartRecorder{}
		r.inject(t)
		a := newAutostartTestApp(t, config.Config{Autostart: true})

		a.syncAutostartOnStartup()
		if r.enableCalls != 1 || r.disableCalls != 0 {
			t.Fatalf("开应调 Enable: enable=%d disable=%d", r.enableCalls, r.disableCalls)
		}
	})
	t.Run("关→Disable", func(t *testing.T) {
		withAutostartEnv(t, true, true)
		r := &autostartRecorder{}
		r.inject(t)
		a := newAutostartTestApp(t, config.Config{})

		a.syncAutostartOnStartup()
		if r.disableCalls != 1 || r.enableCalls != 0 {
			t.Fatalf("关应调 Disable: enable=%d disable=%d", r.enableCalls, r.disableCalls)
		}
	})
	t.Run("dev→零调用", func(t *testing.T) {
		withAutostartEnv(t, false, true)
		r := &autostartRecorder{}
		r.inject(t)
		a := newAutostartTestApp(t, config.Config{Autostart: true})

		a.syncAutostartOnStartup()
		if r.enableCalls != 0 || r.disableCalls != 0 || r.enabledCalls != 0 {
			t.Fatalf("dev 不得动注册表: %+v", r)
		}
	})
	t.Run("不支持→零调用", func(t *testing.T) {
		withAutostartEnv(t, true, false)
		r := &autostartRecorder{}
		r.inject(t)
		a := newAutostartTestApp(t, config.Config{Autostart: true})

		a.syncAutostartOnStartup()
		if r.enableCalls != 0 || r.disableCalls != 0 {
			t.Fatalf("不支持平台不得动注册表: enable=%d disable=%d", r.enableCalls, r.disableCalls)
		}
	})
	t.Run("同步失败不影响启动", func(t *testing.T) {
		withAutostartEnv(t, true, true)
		r := &autostartRecorder{enableErr: errors.New("注册表拒绝")}
		r.inject(t)
		a := newAutostartTestApp(t, config.Config{Autostart: true})

		// 失败仅记日志，不得 panic / 中断调用链（Startup 末尾调用处依赖此语义）。
		a.syncAutostartOnStartup()
		// 失败也不得改写配置。
		a.mu.Lock()
		on := a.opts.Config.Autostart
		a.mu.Unlock()
		if !on {
			t.Fatal("同步失败不应改动内存 config.Autostart")
		}
	})
}
