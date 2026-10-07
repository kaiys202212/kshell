//go:build !windows

package desktop

// showAgentBubble 非 Windows 平台的空实现：主窗不可见时也不弹独立气泡，
// 仅保留 emit 的应用内队列，等窗口恢复后再由前端展示。
func showAgentBubble(title, body, termKey string) error {
	_, _, _ = title, body, termKey
	return nil
}
