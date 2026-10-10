//go:build windows

// 托盘右键菜单的前台权限兜底（Windows 专属）。
//
// 根因：Windows 前台锁只允许"最近接收过输入"的进程调用 SetForegroundWindow 成功。
// 右键托盘图标本会授予前台权限，但托盘消息循环处理 WM_RBUTTONUP 若有延迟
// （托盘线程被 Shell_NotifyIcon 等 SendMessage 短暂阻塞、其他窗口抢先成为前台），
// 授权窗口期已过，energye/systray ShowMenu 内部的 SetForegroundWindow 被系统拒绝，
// TrackPopupMenu 弹不出菜单——即托盘右键偶发无反应。
//
// 兜底：经 AttachThreadInput 与前台线程共享输入态，使随后的 SetForegroundWindow
// 放行。不用"模拟 Alt 键"方案：合成按键会落在他窗焦点上，可能触发对方菜单栏。
// win32 proc 声明复用 window_win.go（同属 windows 构建约束文件）。
package desktop

import "unsafe"

// ensureForegroundThen 做前台权限兜底后弹出菜单（showMenu 通常为 IMenu.ShowMenu，
// 内部执行 SetForegroundWindow + TrackPopupMenu）。无前台窗口或已是本线程时直接透传。
func ensureForegroundThen(showMenu func() error) error {
	var detach func()
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		var pid uint32
		fgThread, _, _ := procGetWindowThreadProcID.Call(fg, uintptr(unsafe.Pointer(&pid)))
		thisThread, _, _ := kernel32GetCurrentThreadID.Call()
		if fgThread != 0 && fgThread != thisThread {
			// TRUE=1：本线程与前台线程挂接，共享输入态从而获得前台置权
			if ok, _, _ := procAttachThreadInput.Call(thisThread, fgThread, 1); ok != 0 {
				detach = func() { procAttachThreadInput.Call(thisThread, fgThread, 0) }
			}
		}
	}
	err := showMenu()
	if detach != nil {
		detach()
	}
	return err
}
