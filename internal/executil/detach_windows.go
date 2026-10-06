//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// Windows 进程创建标志：脱离父 Job，避免 Wails 退出时杀掉自更新/重启助手。
const (
	createBreakawayFromJob = 0x01000000
	createNewProcessGroup  = 0x00000200
)

// DetachFromParent 让子进程脱离当前 Job 且不弹控制台。
func DetachFromParent(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createBreakawayFromJob | createNewProcessGroup,
	}
}
