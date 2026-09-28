//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// createNoWindow 即 CREATE_NO_WINDOW：子进程获得不可见控制台。
// 另设 HideWindow 双保险（两者叠加是社区验证过的稳妥组合）。
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:     true,
		CreationFlags:  createNoWindow,
	}
}
