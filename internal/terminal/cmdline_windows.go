//go:build windows

package terminal

import (
	"syscall"

	"github.com/aymanbagabas/go-pty"
)

// applyCmdLine 把批处理包装的完整命令行写入 SysProcAttr，绕过 ComposeCommandLine。
func applyCmdLine(cmd *pty.Cmd, cmdline string) {
	if cmd == nil || cmdline == "" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = cmdline
}
