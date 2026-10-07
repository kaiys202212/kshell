// 系统托盘支持（internal/desktop 内统一封装，main.go 保持无构建标签）。
// 托盘库选型：github.com/energye/systray（getlantern/systray 的活跃 fork）——
// Windows 走原生 Shell_NotifyIcon，Linux 走纯 Go dbus，macOS 走原生（cgo），
// 三平台 API 一致且 Linux 交叉编译无需 cgo，故不做平台隔离。
// 关闭行为：用户点 X 经 Wails 的 Frontend.Quit 走到 App.BeforeClose，被拦截为隐藏窗口；
// 真正退出只走托盘菜单「退出」（App.quitApp），路径见 quitApp 注释。
package desktop

import (
	"sync"

	"github.com/energye/systray"
	"github.com/yangk/kshell/internal/applang"
)

// trayMenuMu 保护菜单项句柄：托盘 onReady 写入，语言切换时读取就地刷新文案。
var trayMenuMu sync.Mutex

var (
	trayShowItem *systray.MenuItem // 「显示主窗口」菜单项
	trayQuitItem *systray.MenuItem // 「退出」菜单项
)

// refreshTrayText 按当前 applang 语言就地刷新菜单文案（SetTitle 可跨 goroutine 调用）。
// 托盘未启动时句柄为空，无操作；循环退出后句柄被清空，避免操作已销毁的菜单。
func refreshTrayText() {
	trayMenuMu.Lock()
	show, quit := trayShowItem, trayQuitItem
	trayMenuMu.Unlock()
	if show != nil {
		show.SetTitle(applang.T("tray.show_main"))
	}
	if quit != nil {
		quit.SetTitle(applang.T("tray.exit"))
	}
}

// refreshTrayTextFn 是 refreshTrayText 的包级变量抽象：测试注入用
// （对齐 runTrayFn，避免单测触碰真实菜单项句柄）。
var refreshTrayTextFn = refreshTrayText

// clearTrayMenuItems 清空菜单项句柄（托盘循环退出时调用）。
func clearTrayMenuItems() {
	trayMenuMu.Lock()
	trayShowItem, trayQuitItem = nil, nil
	trayMenuMu.Unlock()
}

// trayDispatch 把托盘 UI 回调丢到新 goroutine，避免在 systray wndProc /
// TrackPopupMenu 嵌套泵里同步调用 Wails/Quit 导致消息循环僵死。
var trayDispatch = func(fn func()) {
	if fn == nil {
		return
	}
	go fn()
}

// runTray 启动托盘消息循环（阻塞，调用方需放 goroutine）：
// 菜单「显示主窗口」触发 onShow，「退出」触发 onQuit；文案取自 applang 当前语言。
func runTray(icon []byte, onShow, onQuit func()) {
	systray.Run(func() {
		// Windows 通知区只认 ICO 数据，其它平台用 PNG（图标选择见 main.go trayIcon）
		systray.SetIcon(icon)
		systray.SetTooltip("kshell")
		// 左键单击图标恢复主窗口（WM_LBUTTONUP；缺此回调则左键无任何反应）
		systray.SetOnClick(func(systray.IMenu) {
			trayDispatch(onShow)
		})
		mShow := systray.AddMenuItem(applang.T("tray.show_main"), "显示 kshell 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(applang.T("tray.exit"), "退出 kshell")
		mShow.Click(func() { trayDispatch(onShow) })
		mQuit.Click(func() { trayDispatch(onQuit) })
		trayMenuMu.Lock()
		trayShowItem, trayQuitItem = mShow, mQuit
		trayMenuMu.Unlock()
	}, clearTrayMenuItems)
}

// quitTrayLoop 请求托盘消息循环退出（通知区图标随循环停止被移除）。
// 包级变量便于测试注入；systray.Quit 内部 sync.Once，重复调用安全。
var quitTrayLoop = func() {
	systray.Quit()
}
