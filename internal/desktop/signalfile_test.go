// 退出信号文件测试：存在判断/消费语义与 watcher 全链路（信号 → quitting 置位 → quit）。
package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckExitSignal 验证信号文件的存在判断与消费（读后即删）。
func TestCheckExitSignal(t *testing.T) {
	if checkExitSignal("") {
		t.Fatal("空路径应视为无信号")
	}
	path := filepath.Join(t.TempDir(), "exit.signal")
	if checkExitSignal(path) {
		t.Fatal("文件不存在时不应报告信号")
	}
	if err := os.WriteFile(path, []byte("exit"), 0o600); err != nil {
		t.Fatalf("写信号文件失败: %v", err)
	}
	if !checkExitSignal(path) {
		t.Fatal("文件存在时应报告退出信号")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("信号应被消费（读后即删）")
	}
}

// TestWatchExitSignalQuits 验证 watcher 全链路：信号文件出现 → quitting 置位
// （BeforeClose 放行）→ quitRuntime 被调用（真实进程里接 Wails 退出与 Shutdown 收尾）。
func TestWatchExitSignalQuits(t *testing.T) {
	_, quitCalled := stubRestart(t, nil)
	path := filepath.Join(t.TempDir(), "exit.signal")
	app, _, _ := newTestApp(t)
	app.mu.Lock()
	app.ctx = context.Background()
	app.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		app.watchExitSignal(path, 5*time.Millisecond)
	}()

	if err := os.WriteFile(path, []byte("exit"), 0o600); err != nil {
		t.Fatalf("写信号文件失败: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher 未在超时内响应退出信号")
	}
	if *quitCalled != 1 {
		t.Fatalf("quitRuntime 调用次数 = %d, 期望 1", *quitCalled)
	}
	app.mu.Lock()
	quitting := app.quitting
	app.mu.Unlock()
	if !quitting {
		t.Fatal("信号退出应置位 quitting（放行 BeforeClose）")
	}
}
