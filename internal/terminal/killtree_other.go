//go:build !windows

package terminal

import (
	"syscall"

	"github.com/aymanbagabas/go-pty"
)

// killProcessTree 非 Windows 平台按进程组终止：go-pty 的 unix 实现强制 Setsid
//（子进程是组长），kill(-pgid) 可一并结束 shell 拉起的孙进程。
func killProcessTree(cmd *pty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
