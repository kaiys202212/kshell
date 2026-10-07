//go:build !windows

package desktop

import "errors"

// otherLauncher 是非 Windows 平台的占位弹窗实现：
// 桌面版弹窗目前只支持 Windows Terminal / PowerShell，这里保证包在其他平台可编译。
type otherLauncher struct{}

func (otherLauncher) Launch(dir, title string, args []string) error {
	return errors.New("err.terminal.popup_windows_only")
}

func (otherLauncher) Focus(title string) bool { return false }

func init() {
	defaultLauncherFactory = func() TerminalLauncher { return otherLauncher{} }
}
