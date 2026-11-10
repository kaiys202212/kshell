//go:build windows

package workbuddy

import "syscall"

// sysProcAttrHideWindow 返回隐藏窗口的进程属性（CREATE_NO_WINDOW）。
func sysProcAttrHideWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
