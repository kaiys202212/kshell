//go:build windows

package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultWindowIsForeground：前台窗口属于本进程则视为 kshell 在最前。
// 独立气泡带 WS_EX_NOACTIVATE，不会误占前台。
func defaultWindowIsForeground() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcID.Call(fg, uintptr(unsafe.Pointer(&pid)))
	return pid != 0 && pid == windows.GetCurrentProcessId()
}
