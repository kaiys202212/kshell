//go:build windows

package workbuddy

import "os/exec"

// hideWindow 在 Windows 上隐藏子进程窗口，避免抓 key 时闪黑框。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = sysProcAttrHideWindow()
}
