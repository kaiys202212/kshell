//go:build windows

package desktop

import (
	"golang.org/x/sys/windows"
)

// openLocalFileOS 用 ShellExecute("open") 按文件关联打开（HTML 即系统默认浏览器）。
func openLocalFileOS(abs string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(abs), nil, nil, windows.SW_SHOWNORMAL)
}
