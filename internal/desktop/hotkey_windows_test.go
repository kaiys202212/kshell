//go:build windows

package desktop

import (
	"context"
	"testing"
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

// 最小化：即使前台可见也走显示路径（先还原再聚焦）。
func TestToggleMainWindowShowsWhenMinimised(t *testing.T) {
	a, c := newHotkeyApp(t)
	c.foreground = true
	c.minimised = true

	a.toggleMainWindow()

	if c.show != 1 || c.unminimise != 1 {
		t.Fatalf("最小化应走显示路径: show=%d unminimise=%d", c.show, c.unminimise)
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
