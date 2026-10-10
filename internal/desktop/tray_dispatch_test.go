package desktop

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 托盘右键的前台权限兜底：ensureForegroundThen 必须调用 showMenu 并透传其结果。
// Windows 上该函数内部还会做 AttachThreadInput（无前台窗口时跳过），对测试进程无副作用。
func TestEnsureForegroundThenCallsShowMenu(t *testing.T) {
	calls := 0
	err := ensureForegroundThen(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("不应透传错误, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("showMenu 应被调用 1 次, got %d", calls)
	}
}

func TestEnsureForegroundThenPropagatesError(t *testing.T) {
	sentinel := errors.New("no menu")
	err := ensureForegroundThen(func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("showMenu 错误应原样透传, got %v", err)
	}
}

func TestQuitAppDoesNotCallQuitTrayLoop(t *testing.T) {
	app, _, _ := newTestApp(t)
	trayQuits := 0
	origTray := quitTrayLoop
	quitTrayLoop = func() { trayQuits++ }
	t.Cleanup(func() { quitTrayLoop = origTray })

	origQuit := quitRuntime
	quitRuntime = func(context.Context) {}
	t.Cleanup(func() { quitRuntime = origQuit })

	app.quitApp(context.Background())
	if trayQuits != 0 {
		t.Fatalf("quitApp 不应同步 quitTrayLoop, got %d", trayQuits)
	}
	if !app.quitting {
		t.Fatal("quitApp 应置位 quitting")
	}
	if app.BeforeClose(context.Background()) {
		t.Fatal("quitting 后 BeforeClose 应放行")
	}
}

func TestShutdownCallsQuitTrayLoop(t *testing.T) {
	app, _, _ := newTestApp(t)
	trayQuits := 0
	origTray := quitTrayLoop
	quitTrayLoop = func() { trayQuits++ }
	t.Cleanup(func() { quitTrayLoop = origTray })

	app.Shutdown(context.Background())
	if trayQuits != 1 {
		t.Fatalf("Shutdown 应调用 quitTrayLoop 1 次, got %d", trayQuits)
	}
}

func TestTrayDispatchRunsAsyncByDefault(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		trayDispatch(func() {
			close(started)
			<-release
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("trayDispatch 应立即返回，不应被业务回调阻塞")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("业务回调应在独立 goroutine 执行")
	}
	close(release)
}

func TestRunTrayCallbacksUseTrayDispatch(t *testing.T) {
	calls := 0
	orig := trayDispatch
	trayDispatch = func(fn func()) {
		if fn != nil {
			fn()
			calls++
		}
	}
	t.Cleanup(func() { trayDispatch = orig })

	trayDispatch(func() {})
	if calls != 1 {
		t.Fatalf("注入的 trayDispatch 应被调用, got %d", calls)
	}
	trayDispatch(nil) // 不得 panic
}
