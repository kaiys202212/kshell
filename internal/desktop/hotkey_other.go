//go:build !windows

package desktop

// startHotkey 是系统级热键的空实现：Alt+Shift+K 依赖 Win32 RegisterHotKey，
// 仅 Windows 支持，其它平台静默跳过（见 hotkey_windows.go）。
func startHotkey(a *App) {}

// stopHotkey 与 startHotkey 配对的空实现（无热键线程可停）。
func stopHotkey() {}
