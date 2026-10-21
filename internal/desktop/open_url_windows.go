//go:build windows

package desktop

import "golang.org/x/sys/windows"

// openExternalURLOS 用 ShellExecute("open") 按协议默认应用打开 URL。
func openExternalURLOS(rawURL string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(rawURL), nil, nil, windows.SW_SHOWNORMAL)
}
