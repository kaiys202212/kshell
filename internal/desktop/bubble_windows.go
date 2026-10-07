//go:build windows

package desktop

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 独立通知气泡：右下角 always-on-top toolwindow（非通知中心 toast）。
// 最多同时 3 条堆叠；约 10s 自动关闭；点击经 bubbleActivate 恢复主窗并聚焦。

const (
	bubbleMaxVisible = 3
	bubbleAutoClose  = 10 * time.Second
	bubbleWidth      = 288
	bubbleHeight     = 72
	bubbleMargin     = 16
	bubbleGap        = 8

	bubbleClassName = "KShellAgentBubble"

	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	wsExTopmost    = 0x00000008
	wsExToolwindow = 0x00000080
	wsExNoactivate = 0x08000000

	swpNosize     = 0x0001
	swpNoactivate = 0x0010
	hwndTopmost   = ^uintptr(0) // HWND_TOPMOST = -1

	smCxScreen = 0
	smCyScreen = 1

	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmPaint       = 0x000F
	wmLButtonUp   = 0x0202
	wmTimer       = 0x0113
	wmNCLButtonUp = 0x00A2
	wmNull        = 0x0000

	dtLeft        = 0x0000
	dtWordbreak   = 0x0010
	dtEndEllipsis = 0x00008000

	colorBtnface = 15
	transparent  = 1
	idcArrow     = 32512
	bubbleTimerID = 1
)

var (
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procFillRect         = user32.NewProc("FillRect")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")

	gdi32              = windows.NewLazySystemDLL("gdi32.dll")
	procSetBkMode      = gdi32.NewProc("SetBkMode")
	procSetTextColor   = gdi32.NewProc("SetTextColor")
	procGetStockObject = gdi32.NewProc("GetStockObject")
)

type bubbleMsg struct {
	title, body, termKey string
}

type agentBubble struct {
	hwnd    windows.HWND
	termKey string
	title   string
	body    string
}

type wndClassExW struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type paintStruct struct {
	hdc         windows.Handle
	erase       int32
	rcPaint     rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

type rect struct {
	left, top, right, bottom int32
}

type msgStruct struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

var (
	bubbleMu       sync.Mutex
	bubbleList     []*agentBubble
	bubbleOnce     sync.Once
	bubbleThreadID uint32
	bubbleReady    = make(chan struct{})
	bubbleReqCh    = make(chan bubbleMsg, 8)
)

// showAgentBubble 在右下角弹出一条独立置顶气泡；失败返回 error。
func showAgentBubble(title, body, termKey string) error {
	ensureBubbleUIThread()
	<-bubbleReady
	select {
	case bubbleReqCh <- bubbleMsg{title: title, body: body, termKey: termKey}:
		// 唤醒消息泵（可能正阻塞在 GetMessage）
		bubbleMu.Lock()
		tid := bubbleThreadID
		bubbleMu.Unlock()
		if tid != 0 {
			procPostThreadMessageW.Call(uintptr(tid), wmNull, 0, 0)
		}
		return nil
	default:
		// 队列满时丢弃最新：通知是锦上添花，不能阻塞 dispatcher
		return nil
	}
}

func ensureBubbleUIThread() {
	bubbleOnce.Do(func() {
		go bubbleUILoop()
	})
}

func bubbleUILoop() {
	className, _ := windows.UTF16PtrFromString(bubbleClassName)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	bg, _, _ := procGetStockObject.Call(colorBtnface)

	var wc wndClassExW
	wc.size = uint32(unsafe.Sizeof(wc))
	wc.wndProc = syscall.NewCallback(bubbleWndProc)
	wc.cursor = windows.Handle(cursor)
	wc.background = windows.Handle(bg)
	wc.className = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	bubbleMu.Lock()
	bubbleThreadID = windows.GetCurrentThreadId()
	bubbleMu.Unlock()
	close(bubbleReady)

	var m msgStruct
	for {
		// 先抽干创建请求，再取下一条窗口消息
		drainBubbleRequests()
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		if m.message == wmNull {
			drainBubbleRequests()
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func drainBubbleRequests() {
	for {
		select {
		case req := <-bubbleReqCh:
			createBubbleWindow(req.title, req.body, req.termKey)
		default:
			return
		}
	}
}

func createBubbleWindow(title, body, termKey string) {
	bubbleMu.Lock()
	for len(bubbleList) >= bubbleMaxVisible {
		old := bubbleList[0]
		bubbleList = bubbleList[1:]
		bubbleMu.Unlock()
		procDestroyWindow.Call(uintptr(old.hwnd))
		bubbleMu.Lock()
	}
	bubbleMu.Unlock()

	b := &agentBubble{termKey: termKey, title: title, body: body}
	className, _ := windows.UTF16PtrFromString(bubbleClassName)
	winTitle, _ := windows.UTF16PtrFromString(title)

	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)

	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExTopmost|wsExToolwindow|wsExNoactivate),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winTitle)),
		uintptr(wsPopup|wsVisible),
		uintptr(int32(screenW)-bubbleWidth-bubbleMargin),
		uintptr(int32(screenH)-bubbleHeight-bubbleMargin),
		uintptr(bubbleWidth),
		uintptr(bubbleHeight),
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return
	}
	b.hwnd = windows.HWND(hwnd)
	procSetTimer.Call(hwnd, bubbleTimerID, uintptr(bubbleAutoClose/time.Millisecond), 0)

	bubbleMu.Lock()
	bubbleList = append(bubbleList, b)
	relayoutBubblesLocked(int(screenW), int(screenH))
	bubbleMu.Unlock()

	procUpdateWindow.Call(hwnd)
}

func relayoutBubblesLocked(screenW, screenH int) {
	n := len(bubbleList)
	for i, b := range bubbleList {
		idxFromBottom := n - 1 - i
		x := screenW - bubbleWidth - bubbleMargin
		y := screenH - bubbleMargin - (idxFromBottom+1)*bubbleHeight - idxFromBottom*bubbleGap
		procSetWindowPos.Call(
			uintptr(b.hwnd), hwndTopmost,
			uintptr(x), uintptr(y), 0, 0,
			swpNosize|swpNoactivate,
		)
	}
}

func removeBubble(hwnd windows.HWND) {
	bubbleMu.Lock()
	defer bubbleMu.Unlock()
	for i, b := range bubbleList {
		if b.hwnd == hwnd {
			bubbleList = append(bubbleList[:i], bubbleList[i+1:]...)
			screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
			screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
			relayoutBubblesLocked(int(screenW), int(screenH))
			return
		}
	}
}

// bubbleFromHWND 在 bubbleList 里按 HWND 查找（避免 GWLP_USERDATA + unsafe 指针）。
func bubbleFromHWND(hwnd windows.HWND) *agentBubble {
	bubbleMu.Lock()
	defer bubbleMu.Unlock()
	for _, b := range bubbleList {
		if b.hwnd == hwnd {
			return b
		}
	}
	return nil
}

func bubbleWndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPaint:
		paintBubble(hwnd)
		return 0
	case wmLButtonUp, wmNCLButtonUp:
		if b := bubbleFromHWND(hwnd); b != nil {
			termKey := b.termKey
			procKillTimer.Call(uintptr(hwnd), bubbleTimerID)
			procDestroyWindow.Call(uintptr(hwnd))
			go bubbleActivate(termKey)
		}
		return 0
	case wmTimer:
		if wParam == bubbleTimerID {
			procKillTimer.Call(uintptr(hwnd), bubbleTimerID)
			procDestroyWindow.Call(uintptr(hwnd))
		}
		return 0
	case wmDestroy:
		removeBubble(hwnd)
		return 0
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func paintBubble(hwnd windows.HWND) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var rc rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	bg, _, _ := procGetStockObject.Call(colorBtnface)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)

	b := bubbleFromHWND(hwnd)
	if b == nil {
		return
	}
	procSetBkMode.Call(hdc, transparent)
	procSetTextColor.Call(hdc, 0x00333333)

	pad := int32(12)
	titleRC := rect{left: pad, top: 8, right: rc.right - pad, bottom: 28}
	bodyRC := rect{left: pad, top: 30, right: rc.right - pad, bottom: rc.bottom - 8}

	titlePtr, _ := windows.UTF16PtrFromString(b.title)
	bodyPtr, _ := windows.UTF16PtrFromString(b.body)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(titlePtr)), ^uintptr(0), uintptr(unsafe.Pointer(&titleRC)), dtLeft|dtEndEllipsis)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(bodyPtr)), ^uintptr(0), uintptr(unsafe.Pointer(&bodyRC)), dtLeft|dtWordbreak|dtEndEllipsis)
}
