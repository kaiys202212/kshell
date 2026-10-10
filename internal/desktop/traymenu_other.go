//go:build !windows

// 非 Windows 平台无前台锁问题（Linux 走 dbus 原生菜单、macOS 走 NSStatusItem），
// 直接弹出托盘菜单。Windows 版本的 AttachThreadInput 兜底见 traymenu_windows.go。
package desktop

// ensureForegroundThen 直接透传 showMenu。
func ensureForegroundThen(showMenu func() error) error {
	return showMenu()
}
