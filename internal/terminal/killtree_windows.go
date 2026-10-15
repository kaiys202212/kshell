//go:build windows

package terminal

import (
	"os/exec"
	"strconv"

	"github.com/aymanbagabas/go-pty"

	"github.com/yangk/kshell/internal/executil"
)

// killProcessTree 用 taskkill /T 连带结束后代进程：shell 里经 start 拉起的新控制台
// 进程不受 ConPTY CTRL_CLOSE 管，只杀直属进程会残留孤儿（占用会话锁文件/端口）。
func killProcessTree(cmd *pty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	tk := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	executil.HideWindow(tk) // 关闭页签时不要闪一下黑窗
	_ = tk.Run()
	_ = cmd.Process.Kill()
}
