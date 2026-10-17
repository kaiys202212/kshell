// 桌面版日志落盘：windowsgui 子系统没有控制台，log.Printf 默认写 stderr
// 等于直接丢弃——此前托盘排查（19a74ac 加的弹出失败日志）从未被人看到，
// 根因即此。统一落到 ~/.kshell/logs/desktop.log。
package desktop

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
)

var desktopLogOnce sync.Once

// setupDesktopLog 把标准库 log 重定向到 ~/.kshell/logs/desktop.log。
// 简单轮转：启动时超过 2MB 改名为 desktop.log.old（仅保留一代）。
func setupDesktopLog() {
	desktopLogOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dir := filepath.Join(home, ".kshell", "logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		path := filepath.Join(dir, "desktop.log")
		if fi, err := os.Stat(path); err == nil && fi.Size() > 2<<20 {
			_ = os.Rename(path, path+".old")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		log.SetOutput(f)
		log.Printf("desktop.log 启动 pid=%d", os.Getpid())
		// ACP 协议层的日志钩子（agent 退出码 / stderr 尾部）同走 desktop.log。
		acp.Logf = log.Printf
		// chat 层诊断日志（turn 起止 / 在途工具 / 静默回退）同走 desktop.log。
		chat.Logf = log.Printf
	})
}
