// 退出信号文件：外部脚本构建前创建 ~/.kshell/exit.signal 即可请求运行中的
// 桌面版优雅退出（与托盘「退出」同一条链路：quitting 放行 BeforeClose、
// Shutdown 收掉内嵌终端），供无人值守构建与自动化调试使用。
package desktop

import (
	"os"
	"time"
)

// exitSignalInterval 是信号文件轮询间隔。外部脚本创建文件后要等应用响应，
// 500ms 足够灵敏且轮询成本可忽略（一次 Stat 调用）。
const exitSignalInterval = 500 * time.Millisecond

// checkExitSignal 报告退出信号文件是否存在；存在则顺手删除（消费信号）。
// 返回 true 表示「已收到退出请求」。
func checkExitSignal(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false // 不存在、无权限等一律视为无信号，不打断正常运行
	}
	_ = os.Remove(path)
	return true
}

// watchExitSignal 轮询退出信号文件，发现信号即走 quitApp 优雅退出并返回。
// 轮询与应用同生命周期：进程随 Wails 主循环退出，goroutine 一并结束。
func (a *App) watchExitSignal(path string, interval time.Duration) {
	if path == "" {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if !checkExitSignal(path) {
			continue
		}
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		a.quitApp(ctx)
		return
	}
}
