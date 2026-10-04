// 系统托盘支持（internal/desktop 内统一封装，main.go 保持无构建标签）。
// 托盘库选型：github.com/energye/systray（getlantern/systray 的活跃 fork）——
// Windows 走原生 Shell_NotifyIcon，Linux 走纯 Go dbus，macOS 走原生（cgo），
// 三平台 API 一致且 Linux 交叉编译无需 cgo，故不做平台隔离。
// 关闭行为：用户点 X 经 Wails 的 Frontend.Quit 走到 App.BeforeClose，被拦截为隐藏窗口；
// 真正退出只走托盘菜单「退出」（App.quitApp），路径见 quitApp 注释。
package desktop

import (
	"github.com/energye/systray"
)

// trayDispatch 把托盘 UI 回调丢到新 goroutine，避免在 systray wndProc /
// TrackPopupMenu 嵌套泵里同步调用 Wails/Quit 导致消息循环僵死。
var trayDispatch = func(fn func()) {
	if fn == nil {
		return
	}
	go fn()
}

// runTray 启动托盘消息循环（阻塞，调用方需放 goroutine）：
// 菜单「显示主窗口」触发 onShow，「退出」触发 onQuit。
func runTray(icon []byte, onShow, onQuit func()) {
	systray.Run(func() {
		// Windows 通知区只认 ICO 数据，其它平台用 PNG（图标选择见 main.go trayIcon）
		systray.SetIcon(icon)
		systray.SetTooltip("kshell")
		// 左键单击图标恢复主窗口（WM_LBUTTONUP；缺此回调则左键无任何反应）
		systray.SetOnClick(func(systray.IMenu) {
			trayDispatch(onShow)
		})
		mShow := systray.AddMenuItem("显示主窗口", "显示 kshell 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 kshell")
		mShow.Click(func() { trayDispatch(onShow) })
		mQuit.Click(func() { trayDispatch(onQuit) })
	}, nil)
}

// quitTrayLoop 请求托盘消息循环退出（通知区图标随循环停止被移除）。
// 包级变量便于测试注入；systray.Quit 内部 sync.Once，重复调用安全。
var quitTrayLoop = func() {
	systray.Quit()
}
