//go:build !windows

package desktop

// defaultWindowIsForeground 非 Windows 无独立气泡实现，恒 true，
// 保持「仅托盘/最小化时尝试弹气泡（空实现）」的既有行为。
func defaultWindowIsForeground() bool { return true }
