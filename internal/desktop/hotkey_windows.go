//go:build windows

package desktop

import (
	"log"
	"sync"
	"unsafe"
)

// 系统级热键 Alt+Shift+K（Windows）：message-only window + RegisterHotKey，
// 消息循环收到 WM_HOTKEY 后切换主窗口显隐。键位被占用（RegisterHotKey 失败）
// 或建窗失败时仅写日志静默降级，不影响托盘等其它路径。
const (
	hotkeyID          = 1
	modAlt            = 0x1
	modShift          = 0x4
	modNoRepeat       = 0x4000
	hotkeyVK          = 'K'
	hwndMessage       = 3 // HWND_MESSAGE：message-only window
	wmHotkey          = 0x0312
	wmQuit            = 0x0012
	hotkeyThreadLabel = "hotkey"
)

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
)

// hotkeyState 保存热键线程信息：stopHotkey 需要向该线程 post WM_QUIT，
// 而 thread id 只有热键 goroutine 自己知道（GetCurrentThreadId），用锁共享。
var hotkeyState struct {
	mu       sync.Mutex
	threadID uintptr
}

// hotkeyRunner 把「注册热键 + 消息循环」抽成包级变量：单测注入 fake 直接
// 验证 WM_HOTKEY 回调行为，不真跑 Win32 消息循环。
var hotkeyRunner = func(a *App) {
	runHotkeyLoop(a)
}

// startHotkey 在独立 goroutine 里注册热键并跑消息循环（Startup 末尾调用）。
// 每个 App 生命周期只调用一次；热键线程 id 记入 hotkeyState 供 stopHotkey 使用。
func startHotkey(a *App) {
	go func() {
		hotkeyState.mu.Lock()
		hotkeyState.threadID, _, _ = kernel32GetCurrentThreadID.Call()
		hotkeyState.mu.Unlock()
		hotkeyRunner(a)
	}()
}

// stopHotkey 向热键线程 post WM_QUIT 结束消息循环（Shutdown 开头调用）。
// 线程 id 未知（未启动/已退出）时静默跳过；UnregisterHotKey 由循环退出路径
// 统一执行，避免与 GetMessage 竞争。
func stopHotkey() {
	hotkeyState.mu.Lock()
	tid := hotkeyState.threadID
	hotkeyState.mu.Unlock()
	if tid == 0 {
		return
	}
	procPostThreadMessageW.Call(tid, wmQuit, 0, 0)
}

// runHotkeyLoop 真实实现：建 message-only window、注册热键、跑消息循环。
// 失败路径全部静默降级（log.Printf 后返回），热键不可用不阻塞应用启动。
func runHotkeyLoop(a *App) {
	// message-only window 不显示、不进任务栏，仅用于接收 WM_HOTKEY。
	// 窗口类/标题/参数全部为默认值即可，系统对 HWND_MESSAGE 子窗口要求宽松。
	hwnd, _, _ := procCreateWindowExW.Call(
		0, 0, 0, 0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		log.Printf("热键窗口创建失败，Alt+Shift+K 不可用")
		return
	}
	if ret, _, _ := procRegisterHotKey.Call(hwnd, hotkeyID,
		uintptr(modAlt|modShift|modNoRepeat), uintptr(hotkeyVK)); ret == 0 {
		log.Printf("注册热键 Alt+Shift+K 失败（可能被其它程序占用），已跳过")
		procDestroyWindow.Call(hwnd)
		return
	}
	defer func() {
		procUnregisterHotKey.Call(hwnd, hotkeyID)
		procDestroyWindow.Call(hwnd)
	}()

	var msg [3]uintptr // MSG 结构体：hwnd/message/wParam/lParam/time/pt，按 uintptr 对齐
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		switch int32(ret) {
		case -1: // 错误：直接退出循环走清理
			return
		case 0: // WM_QUIT
			return
		}
		if msg[1] == wmHotkey { // msg[1] 是 message 字段
			a.toggleMainWindow()
		}
	}
}
