//go:build !windows

package desktop

// runningProcessUnderDir：非 Windows 暂不实现进程枚举，返回 false（不阻塞修复）。
func runningProcessUnderDir(string) bool { return false }
