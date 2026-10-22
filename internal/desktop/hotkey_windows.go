//go:build windows

package desktop

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 系统级热键 Alt+Shift+K（Windows）：message-only window + RegisterHotKey，
// 消息循环收到 WM_HOTKEY 后切换主窗口显隐。键位被占用（RegisterHotKey 失败）
// 或建窗失败时仅写日志静默降级，不影响托盘等其它路径。
const (
	hotkeyID          = 1
	modAlt            = 0x1
	modShift          = 0x4
	modNoRepeat       = 0x4000
	hotkeyVK = 'K'
	// hwndMessage 即 Win32 HWND_MESSAGE ((HWND)-3)。切勿写成字面量 3：
	// 父句柄错误时 CreateWindowExW 必然失败，热键全程不可用。
	hwndMessage = ^uintptr(2)
	wmHotkey    = 0x0312
	wmQuit      = 0x0012

	// hotkeyClassName 用系统预注册类 "Static"：Win32 要求 lpClassName 非 NULL，
	// 传 NULL 时 CreateWindowExW 必然失败；message-only window 对类要求宽松，
	// 无需仿 bubble 走 RegisterClassExW 自注册。
	hotkeyClassName = "Static"
)

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
)

// hotkeyState 保存热键线程信息：stopHotkey 需要向该线程 post WM_QUIT，
// 而线程 id 只有热键 goroutine 自己知道（LockOSThread 后取），用锁共享。
// runner 返回后清零：Windows 线程 id 可复用，残留旧值会误伤无关线程。
var hotkeyState struct {
	mu       sync.Mutex
	threadID uintptr
}

// hotkeyRunner 把「注册热键 + 消息循环」抽成包级变量：单测注入 fake 直接
// 验证 WM_HOTKEY 回调行为，不真跑 Win32 消息循环。
var hotkeyRunner = func(a *App) {
	runHotkeyLoop(a)
}

// startHotkey 注册热键并跑消息循环，阻塞至 WM_QUIT/出错（调用方需放 goroutine：
// app.go Startup 里 `go startHotkey(a)`，本函数即 goroutine 主体，不再嵌套）。
// 每个 App 生命周期只调用一次；线程 id 记入 hotkeyState 供 stopHotkey 使用。
// 必须 LockOSThread：消息循环与建窗线程绑定（同 tray.go / bubble_windows.go），
// goroutine 被调度迁移后 GetMessage 泵的是新线程队列，WM_HOTKEY 永远收不到。
func startHotkey(a *App) {
	runtime.LockOSThread()
	hotkeyState.mu.Lock()
	hotkeyState.threadID = uintptr(windows.GetCurrentThreadId()) // 钉死后取，与建窗/泵消息线程一致
	hotkeyState.mu.Unlock()
	hotkeyRunner(a)
	hotkeyState.mu.Lock()
	hotkeyState.threadID = 0
	hotkeyState.mu.Unlock()
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
	className, _ := windows.UTF16PtrFromString(hotkeyClassName)
	hwnd, _, createErr := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		log.Printf("热键窗口创建失败（errno=%v），Alt+Shift+K 不可用", createErr)
		return
	}
	if ret, _, regErr := procRegisterHotKey.Call(hwnd, hotkeyID,
		uintptr(modAlt|modShift|modNoRepeat), uintptr(hotkeyVK)); ret == 0 {
		log.Printf("注册热键 Alt+Shift+K 失败（errno=%v，可能被其它程序占用），已跳过", regErr)
		procDestroyWindow.Call(hwnd)
		return
	}
	defer func() {
		procUnregisterHotKey.Call(hwnd, hotkeyID)
		procDestroyWindow.Call(hwnd)
	}()

	// MSG 结构体必须用完整定义（bubble_windows.go 的 msgStruct）：
	// x64 上 GetMessageW 写满 48 字节，手写 [3]uintptr 只有 24 字节会溢出栈。
	var m msgStruct
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(ret) {
		case -1: // 错误：直接退出循环走清理
			return
		case 0: // WM_QUIT
			return
		}
		if m.message == wmHotkey {
			a.toggleMainWindow()
		}
	}
}
