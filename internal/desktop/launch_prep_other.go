//go:build !windows

package desktop

// 非 Windows：桌面升级竞态主要发生在 Win32 Mutex/WebView2 路径，此处为空操作。
func listDesktopPeerPIDs() ([]int, error) { return nil, nil }

func singleInstanceMutexFree() bool { return true }

func killProcessIDs(pids []int) error { return nil }
