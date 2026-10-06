//go:build !windows

package desktop

// showAgentToast 非 Windows 平台的空实现：agent 通知只走应用内气泡（emit）。
func showAgentToast(title, body string) error { return nil }
