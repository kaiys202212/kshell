package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestShutdownTimesOutHungCleanup：关终端等清理卡住时，Shutdown 必须在超时后返回，
// 否则 Wails OnShutdown 永不结束，托盘图标已消但进程残留在任务管理器。
func TestShutdownTimesOutHungCleanup(t *testing.T) {
	origTO := shutdownCleanupTimeout
	origCleanup := runShutdownCleanup
	shutdownCleanupTimeout = 40 * time.Millisecond
	runShutdownCleanup = func(*App) { select {} } // 永挂
	t.Cleanup(func() {
		shutdownCleanupTimeout = origTO
		runShutdownCleanup = origCleanup
	})

	app, _, _ := newTestApp(t)
	origTray := quitTrayLoop
	trayQuits := 0
	quitTrayLoop = func() { trayQuits++ }
	t.Cleanup(func() { quitTrayLoop = origTray })

	start := time.Now()
	app.Shutdown(context.Background())
	elapsed := time.Since(start)

	if trayQuits != 1 {
		t.Fatalf("仍应先 quitTrayLoop, got %d", trayQuits)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Shutdown 应在超时后返回, elapsed=%v", elapsed)
	}
	if elapsed < 30*time.Millisecond {
		t.Fatalf("应等到超时, elapsed=%v", elapsed)
	}
}

// TestQuitAppSchedulesForceExit：托盘退出后若 Shutdown 卡住，定时强制 os.Exit 兜底。
func TestQuitAppSchedulesForceExit(t *testing.T) {
	app, _, _ := newTestApp(t) // 先建（会 stub 掉 scheduleForceExit），再覆盖为 spy

	var scheduled atomic.Int64
	scheduleForceExit = func(d time.Duration) {
		scheduled.Store(int64(d))
	}

	origQuit := quitRuntime
	quitRuntime = func(context.Context) {}
	t.Cleanup(func() { quitRuntime = origQuit })

	app.quitApp(context.Background())

	if got := time.Duration(scheduled.Load()); got != forceExitDelay {
		t.Fatalf("应调度 forceExit delay=%v, got %v", forceExitDelay, got)
	}
}
