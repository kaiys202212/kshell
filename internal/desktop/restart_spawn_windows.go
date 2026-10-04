//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// restartWaitStartPowerShell 生成父进程退出后再启动 exe 的 PowerShell 脚本。
func restartWaitStartPowerShell(pid int, exe string) string {
	lit := strings.ReplaceAll(exe, "'", "''")
	// 仅隐藏 PowerShell 宿主窗口（见 spawnSelfDelayedImpl）；新实例必须 Normal，否则主窗被藏起。
	return fmt.Sprintf(
		"Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Process -LiteralPath '%s'",
		pid, lit,
	)
}

// spawnSelfDelayedImpl 在当前进程退出释放单实例 mutex 后再启动自身。
func spawnSelfDelayedImpl(exe string) error {
	ps := restartWaitStartPowerShell(os.Getpid(), exe)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	return cmd.Start()
}
