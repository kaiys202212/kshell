// 系统托盘支持（internal/desktop 内统一封装，main.go 保持无构建标签）。
// 托盘库选型：github.com/energye/systray（getlantern/systray 的活跃 fork）——
// Windows 走原生 Shell_NotifyIcon，Linux 走纯 Go dbus，macOS 走原生（cgo），
// 三平台 API 一致且 Linux 交叉编译无需 cgo，故不做平台隔离。
// 关闭行为：用户点 X 经 Wails 的 Frontend.Quit 走到 App.BeforeClose，被拦截为隐藏窗口；
// 真正退出只走托盘菜单「退出」（App.quitApp），路径见 quitApp 注释。
package desktop

import (
	"log"
	"runtime"
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

// refreshTrayText 按当前 applang 语言就地刷新菜单标题与 tooltip
// （SetTitle/SetTooltip 均可跨 goroutine 调用，SetTooltip 走同一 update 通道，
// 运行时刷新受支持）。
// 托盘未启动时句柄为空，无操作；循环退出后句柄被清空，避免操作已销毁的菜单。
func refreshTrayText() {
	trayMenuMu.Lock()
	show, quit := trayShowItem, trayQuitItem
	trayMenuMu.Unlock()
	if show != nil {
		show.SetTitle(applang.T("tray.show_main"))
		show.SetTooltip(applang.T("tray.show_main_tip"))
	}
	if quit != nil {
		quit.SetTitle(applang.T("tray.exit"))
		quit.SetTooltip(applang.T("tray.exit_tip"))
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
	// 必须把本 goroutine 钉死在当前 OS 线程：托盘窗口由本 goroutine 创建，
	// Windows 的 GetMessage 只泵「当前线程」的消息队列——若 Go 调度器在两次
	// doNativeTick 之间把未锁线程的 goroutine 迁到别的线程，消息循环从此
	// 泵错队列，托盘右键/左键/看门狗 WM_NULL 全部永远无响应
	// （2026-10-14 黑匣子栈 dump 证实：单次 GetMessage 卡 342 分钟）。
	// getlantern/energye systray 文档同样要求非主 goroutine 调用 Run 前锁线程；
	// Run 阻塞至 Quit 返回、本 goroutine 随即结束，无需 Unlock。
	runtime.LockOSThread()
	systray.Run(func() {
		// Windows 通知区只认 ICO 数据，其它平台用 PNG（图标选择见 main.go trayIcon）
		systray.SetIcon(icon)
		systray.SetTooltip("Capybara")
		// 左键单击图标恢复主窗口（WM_LBUTTONUP；缺此回调则左键无任何反应）
		systray.SetOnClick(func(systray.IMenu) {
			trayDispatch(onShow)
		})
		// 右键接管：库默认在 wndProc 里直接 ShowMenu，遇前台锁（SetForegroundWindow
		// 被拒）会弹不出菜单。这里先做前台权限兜底再弹（traymenu_windows.go）。
		// 在托盘 wndProc 线程同步执行属库设计使然，TrackPopupMenu 自带模态循环；
		// macOS 注册后行为与默认 show_menu 等价，Linux dbus 菜单原生弹出不受影响。
		systray.SetOnRClick(func(m systray.IMenu) {
			// 弹出失败记日志（落盘 ~/.kshell/logs/desktop.log，见 logfile.go）便于诊断残余场景；
			// 库默认路径同样忽略该错误。用户点空白取消菜单返回的 ERROR_SUCCESS 不算失败。
			err := ensureForegroundThen(m.ShowMenu)
			// KB135788：TrackPopupMenu 返回后补发 WM_NULL，防止菜单态残留吞掉下一次右键
			postTrayWMNull()
			if err != nil && !showMenuErrBenign(err) {
				log.Printf("systray: 托盘右键弹出菜单失败: %v", err)
			}
		})
		mShow := systray.AddMenuItem(applang.T("tray.show_main"), applang.T("tray.show_main_tip"))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(applang.T("tray.exit"), applang.T("tray.exit_tip"))
		mShow.Click(func() { trayDispatch(onShow) })
		mQuit.Click(func() { trayDispatch(onQuit) })
		trayMenuMu.Lock()
		trayShowItem, trayQuitItem = mShow, mQuit
		trayMenuMu.Unlock()
		// 句柄赋值后补刷一次：闭合「菜单创建/赋值期间 SetLanguage 抢先」的窗口
		// （那次刷新会因句柄为空而落空，此处按最新语言兜底）。
		refreshTrayText()
	}, clearTrayMenuItems)
}

// quitTrayLoop 请求托盘消息循环退出（通知区图标随循环停止被移除）。
// 包级变量便于测试注入；systray.Quit 内部 sync.Once，重复调用安全。
var quitTrayLoop = func() {
	systray.Quit()
}
