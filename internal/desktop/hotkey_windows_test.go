//go:build windows

package desktop

import (
	"context"
	"testing"
	"time"
)

// stubHotkeyWindow 一次性替换 toggleMainWindow 用到的窗口抽象，
// 返回记录各调用次数的结构体，测试结束恢复原实现。
func stubHotkeyWindow(t *testing.T) *hotkeyWindowCalls {
	t.Helper()
	c := &hotkeyWindowCalls{}
	origHide, origShow, origUnmin := windowHide, windowShow, windowUnminimise
	origMin, origFg := windowIsMinimised, windowIsForeground
	windowHide = func(context.Context) { c.hide++ }
	windowShow = func(context.Context) { c.show++ }
	windowUnminimise = func(context.Context) { c.unminimise++ }
	windowIsMinimised = func(context.Context) bool { return c.minimised }
	windowIsForeground = func() bool { return c.foreground }
	t.Cleanup(func() {
		windowHide, windowShow, windowUnminimise = origHide, origShow, origUnmin
		windowIsMinimised, windowIsForeground = origMin, origFg
	})
	return c
}

type hotkeyWindowCalls struct {
	hide, show, unminimise int
	minimised, foreground  bool
}

// newHotkeyApp 组装只关心窗口 toggle 的 App（ctx 就绪，与真实 Startup 后一致）。
func newHotkeyApp(t *testing.T) (*App, *hotkeyWindowCalls) {
	t.Helper()
	c := stubHotkeyWindow(t)
	a := NewAppWith(Options{Emit: func(string, ...any) {}})
	a.ctx = context.Background()
	return a, c
}

func hotkeyHidden(a *App) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.windowHidden
}

// 可见 + 前台 + 非最小化：toggle 应隐藏窗口并置 windowHidden。
func TestToggleMainWindowHidesWhenVisibleForeground(t *testing.T) {
	a, c := newHotkeyApp(t)
	c.foreground = true

	a.toggleMainWindow()

	if c.hide != 1 {
		t.Fatalf("windowHide 调用 %d 次, want 1", c.hide)
	}
	if c.show != 0 || c.unminimise != 0 {
		t.Fatalf("隐藏路径不应调用 show/unminimise: show=%d unminimise=%d", c.show, c.unminimise)
	}
	if !hotkeyHidden(a) {
		t.Fatal("toggle 后 windowHidden 应为 true")
	}
}

// 隐藏中（如已收进托盘）：toggle 应显示窗口并清除 windowHidden。
func TestToggleMainWindowShowsWhenHidden(t *testing.T) {
	a, c := newHotkeyApp(t)
	a.setWindowHidden(true)

	a.toggleMainWindow()

	if c.show != 1 {
		t.Fatalf("windowShow 调用 %d 次, want 1", c.show)
	}
	if c.hide != 0 {
		t.Fatalf("显示路径不应调用 windowHide, got %d", c.hide)
	}
	if hotkeyHidden(a) {
		t.Fatal("toggle 后 windowHidden 应为 false")
	}
}

// 最小化：即使前台可见也走显示路径，且只走 SC_RESTORE 还原（unminimise）——
// 对最大化窗口 windowShow 之外的 SC_RESTORE 已足够，且不会解除最大化
// （对齐 activateFromBubble：最小化与普通显示不可连调）。
func TestToggleMainWindowShowsWhenMinimised(t *testing.T) {
	a, c := newHotkeyApp(t)
	c.foreground = true
	c.minimised = true

	a.toggleMainWindow()

	if c.unminimise != 1 {
		t.Fatalf("最小化应走 unminimise 还原, got %d", c.unminimise)
	}
	if c.show != 0 {
		t.Fatalf("最小化路径不应再调用 windowShow（SC_RESTORE 已前置且保留最大化）, got %d", c.show)
	}
	if c.hide != 0 {
		t.Fatalf("最小化不应调用 windowHide, got %d", c.hide)
	}
	if hotkeyHidden(a) {
		t.Fatal("toggle 后 windowHidden 应为 false")
	}
}

// 非前台：窗口可见但被别的应用盖住，toggle 应把它拉回前台。
func TestToggleMainWindowShowsWhenNotForeground(t *testing.T) {
	a, c := newHotkeyApp(t)

	a.toggleMainWindow()

	if c.show != 1 {
		t.Fatalf("非前台应走显示路径: show=%d", c.show)
	}
	if c.hide != 0 {
		t.Fatalf("非前台不应调用 windowHide, got %d", c.hide)
	}
	if hotkeyHidden(a) {
		t.Fatal("toggle 后 windowHidden 应为 false")
	}
}

// ctx 未就绪时安全返回，不 panic、不触碰任何窗口操作。
func TestToggleMainWindowNilContextSafe(t *testing.T) {
	a, c := newHotkeyApp(t)
	a.mu.Lock()
	a.ctx = nil
	a.mu.Unlock()

	a.toggleMainWindow() // 不应 panic

	if c.hide != 0 || c.show != 0 || c.unminimise != 0 {
		t.Fatalf("ctx 为 nil 不应触碰窗口操作: hide=%d show=%d unminimise=%d",
			c.hide, c.show, c.unminimise)
	}
}

// stubHotkeyRunner 临时替换 hotkeyRunner，返回记录调用次数的钩子。
func stubHotkeyRunner(t *testing.T, fn func(*App)) *int {
	t.Helper()
	calls := 0
	orig := hotkeyRunner
	hotkeyRunner = func(a *App) {
		calls++
		if fn != nil {
			fn(a)
		}
	}
	t.Cleanup(func() { hotkeyRunner = orig })
	return &calls
}

// fakeHotkeyApp 组装最小 App 供热键线程生命周期测试使用（不触碰窗口）。
func fakeHotkeyApp() *App {
	return NewAppWith(Options{Emit: func(string, ...any) {}})
}

// stopHotkey 在热键未启动（tid=0）时必须安全跳过，不 panic、不误发消息。
func TestStopHotkeyWithoutStart(t *testing.T) {
	hotkeyState.mu.Lock()
	orig := hotkeyState.threadID
	hotkeyState.threadID = 0
	hotkeyState.mu.Unlock()
	t.Cleanup(func() {
		hotkeyState.mu.Lock()
		hotkeyState.threadID = orig
		hotkeyState.mu.Unlock()
	})

	stopHotkey() // 不应 panic
}

// startHotkey 把 hotkeyRunner 丢进独立 goroutine，tid 记入 hotkeyState；
// runner 返回后 hotkeyState 的 tid 必须清零（Windows 线程 id 可复用，
// 残留旧值会让 stopHotkey 向无关线程 post WM_QUIT）。
func TestStartHotkeyClearsThreadIDAfterRunnerReturns(t *testing.T) {
	calls := stubHotkeyRunner(t, func(a *App) {
		hotkeyState.mu.Lock()
		if hotkeyState.threadID == 0 {
			hotkeyState.mu.Unlock()
			t.Error("runner 执行时 tid 应已记录")
			return
		}
		hotkeyState.mu.Unlock()
	})
	a := fakeHotkeyApp()

	startHotkey(a)

	// 等 runner 跑完（tid 记录与清零都在 runner 前后同步完成）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hotkeyState.mu.Lock()
		tid := hotkeyState.threadID
		hotkeyState.mu.Unlock()
		if *calls == 1 && tid == 0 {
			return // runner 已执行且 tid 已清零
		}
		time.Sleep(5 * time.Millisecond)
	}
	hotkeyState.mu.Lock()
	tid := hotkeyState.threadID
	hotkeyState.mu.Unlock()
	t.Fatalf("runner 调用 %d 次, tid=%d; 期望 runner 执行且 tid 清零", *calls, tid)
}
