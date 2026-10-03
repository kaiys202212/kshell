package desktop

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// stubWindowHide 替换 windowHide 并记录调用次数。
func stubWindowHide(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := windowHide
	windowHide = func(context.Context) { calls++ }
	t.Cleanup(func() { windowHide = orig })
	return &calls
}

// newCloseApp 组装一个只关心关闭行为的 App：带临时 Layout、指定行为与托盘状态。
func newCloseApp(t *testing.T, behavior string, trayActive bool) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.CloseBehavior = behavior
	app := NewAppWith(Options{
		Config: cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
	})
	app.trayActive = trayActive
	return app
}

func TestBeforeCloseHidesWhenTrayModeAndActive(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, true)

	if got := app.BeforeClose(context.Background()); !got {
		t.Fatal("tray 模式应拦截关闭（返回 true）")
	}
	if *calls != 1 {
		t.Fatalf("windowHide 调用 %d 次, want 1", *calls)
	}
}

func TestBeforeCloseExitsWhenBehaviorExit(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorExit, true)

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("exit 模式应放行（返回 false）")
	}
	if *calls != 0 {
		t.Fatalf("exit 模式不应调用 windowHide, got %d", *calls)
	}
}

func TestBeforeCloseExitsWhenTrayInactive(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, false)

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("托盘未激活时应直接退出（返回 false）")
	}
	if *calls != 0 {
		t.Fatalf("托盘未激活不应调用 windowHide, got %d", *calls)
	}
}

func TestBeforeCloseExitsWhenQuitting(t *testing.T) {
	calls := stubWindowHide(t)
	app := newCloseApp(t, config.CloseBehaviorTray, true)
	app.mu.Lock()
	app.quitting = true
	app.mu.Unlock()

	if got := app.BeforeClose(context.Background()); got {
		t.Fatal("quitting 应放行")
	}
	if *calls != 0 {
		t.Fatalf("quitting 不应调用 windowHide, got %d", *calls)
	}
}

func TestGetSetCloseBehaviorPersists(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	app := NewAppWith(Options{Config: config.Default(), Layout: layout})

	if got := app.GetCloseBehavior(); got != config.CloseBehaviorTray {
		t.Fatalf("默认 GetCloseBehavior = %q, want tray", got)
	}
	if err := app.SetCloseBehavior(config.CloseBehaviorExit); err != nil {
		t.Fatalf("SetCloseBehavior: %v", err)
	}
	if got := app.GetCloseBehavior(); got != config.CloseBehaviorExit {
		t.Fatalf("GetCloseBehavior = %q, want exit", got)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CloseBehavior != config.CloseBehaviorExit {
		t.Fatalf("persisted = %q, want exit", loaded.CloseBehavior)
	}

	if err := app.SetCloseBehavior("bogus"); err == nil {
		t.Fatal("非法值应返回错误")
	}
	if got := app.GetCloseBehavior(); got != config.CloseBehaviorExit {
		t.Fatalf("非法值不应改变当前行为, got %q", got)
	}
}

func TestSetCloseBehaviorNotReady(t *testing.T) {
	app := NewAppWith(Options{Config: config.Default()})
	if err := app.SetCloseBehavior(config.CloseBehaviorExit); err == nil {
		t.Fatal("未装配 Layout 应返回错误")
	}
}

func TestGetCloseBehaviorFallsBackToTray(t *testing.T) {
	for _, in := range []string{"", "EXIT", "bogus"} {
		app := NewAppWith(Options{Config: config.Config{CloseBehavior: in}})
		if got := app.GetCloseBehavior(); got != config.CloseBehaviorTray {
			t.Fatalf("GetCloseBehavior(%q) = %q, want tray", in, got)
		}
	}
}

func TestSettersPreserveEachOthersFields(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	app := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(string, ...any) {},
	})

	if err := app.SetAppearanceMode("dark"); err != nil {
		t.Fatalf("SetAppearanceMode: %v", err)
	}
	if err := app.SetCloseBehavior(config.CloseBehaviorExit); err != nil {
		t.Fatalf("SetCloseBehavior: %v", err)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Appearance.Mode != "dark" {
		t.Fatalf("appearance mode lost: %q", loaded.Appearance.Mode)
	}
	if loaded.CloseBehavior != config.CloseBehaviorExit {
		t.Fatalf("close behavior lost: %q", loaded.CloseBehavior)
	}
}

func TestStartTrayActivatesGuard(t *testing.T) {
	orig := runTrayFn
	started := make(chan struct{})
	runTrayFn = func([]byte, func(), func()) { close(started) }
	t.Cleanup(func() { runTrayFn = orig })

	app := NewAppWith(Options{TrayIcon: []byte{1}})
	app.ctx = context.Background()
	app.StartTray()
	<-started

	app.mu.Lock()
	active := app.trayActive
	app.mu.Unlock()
	if !active {
		t.Fatal("StartTray 就绪后应置位 trayActive")
	}
}

func TestStartTraySkippedWithoutIcon(t *testing.T) {
	orig := runTrayFn
	called := false
	runTrayFn = func([]byte, func(), func()) { called = true }
	t.Cleanup(func() { runTrayFn = orig })

	app := NewAppWith(Options{})
	app.ctx = context.Background()
	app.StartTray()

	app.mu.Lock()
	active := app.trayActive
	app.mu.Unlock()
	if called || active {
		t.Fatal("无托盘图标时不应启动托盘消息循环，也不应置位 trayActive")
	}
}
