//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	defaultProcAlive = windowsProcAlive
}

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procIsIconic               = user32.NewProc("IsIconic")
	procGetWindowThreadProcID  = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput      = user32.NewProc("AttachThreadInput")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procSwitchToThisWindow     = user32.NewProc("SwitchToThisWindow")
	procKeybdEvent             = user32.NewProc("keybd_event")
	kernel32GetCurrentThreadID = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

const (
	swRestore       = 9
	vkMenu          = 0x12 // ALT
	keyeventfKeyUp  = 0x2
	wmTitleMaxChars = 1024
)

// windowsLauncher 基于 Windows Terminal / PowerShell 的 TerminalLauncher 实现。
type windowsLauncher struct{}

// NewWindowsLauncher 返回 Win32 终端弹窗/聚焦实现。
func NewWindowsLauncher() TerminalLauncher {
	return &windowsLauncher{}
}

// findWt 定位 wt.exe：先 PATH，再回退 %LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe。
// 找不到返回空串。
func findWt() string {
	if p, err := exec.LookPath("wt.exe"); err == nil {
		return p
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		cand := local + `\Microsoft\WindowsApps\wt.exe`
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

// Launch 弹出新终端窗口。
// Windows Terminal 存在时：`wt -w new nt --title <title> powershell -NoExit -Command ...`；
// 否则回退 powershell -NoExit，用 $host.UI.RawUI.WindowTitle 设置标题。
// 工作目录双保险：cmd.Dir 设置启动进程自身工作目录（wt 通常会把启动器 cwd
// 传给新标签页的 shell），脚本内再 Set-Location 显式切换，规避个别 wt
// 版本不继承 cwd 的差异。
func (l *windowsLauncher) Launch(dir, title string, args []string) error {
	inner := "Set-Location -LiteralPath " + psQuote(dir)
	for _, a := range args {
		inner += "; " + a
	}
	if wtPath := findWt(); wtPath != "" {
		cmd := exec.Command(wtPath, "-w", "new", "nt", "--title", title,
			"powershell", "-NoExit", "-Command", inner)
		cmd.Dir = dir
		return cmd.Start()
	}
	script := fmt.Sprintf("$host.UI.RawUI.WindowTitle=%s; %s", psQuote(title), inner)
	cmd := exec.Command("powershell", "-NoExit", "-Command", script)
	cmd.Dir = dir
	return cmd.Start()
}

// Focus 按标题精确匹配窗口并聚焦（最小化先还原）。
// SetForegroundWindow 失败时依次尝试 SwitchToThisWindow、
// AttachThreadInput、ALT 技巧（Windows 前台锁定限制的兜底链）。
// 只要找到窗口即返回 true（兜底为尽力而为）。
func (l *windowsLauncher) Focus(title string) bool {
	hwnd := findWindowByTitle(title)
	if hwnd == 0 {
		return false
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	if ret, _, _ := procSetForegroundWindow.Call(hwnd); ret != 0 {
		return true
	}
	// 兜底 1：SwitchToThisWindow（未文档化但长期可用）
	procSwitchToThisWindow.Call(hwnd, 1)
	if ret, _, _ := procSetForegroundWindow.Call(hwnd); ret != 0 {
		return true
	}
	// 兜底 2：AttachThreadInput 把当前线程附加到前台线程，绕过前台锁定
	thisThread, _, _ := kernel32GetCurrentThreadID.Call()
	fgHwnd, _, _ := procGetForegroundWindow.Call()
	fgThread, _, _ := procGetWindowThreadProcID.Call(fgHwnd, 0)
	if fgThread != 0 && fgThread != thisThread {
		if ret, _, _ := procAttachThreadInput.Call(thisThread, fgThread, 1); ret != 0 {
			procSetForegroundWindow.Call(hwnd)
			procAttachThreadInput.Call(thisThread, fgThread, 0)
		}
	}
	if ret, _, _ := procSetForegroundWindow.Call(hwnd); ret != 0 {
		return true
	}
	// 兜底 3：ALT 技巧（模拟按下并释放 ALT，解除前台锁定后聚焦）
	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
	procSetForegroundWindow.Call(hwnd)
	return true
}

// windowsProcAlive 按标题查找窗口，找到即存活。
func windowsProcAlive(title string) bool {
	return findWindowByTitle(title) != 0
}

// findWindowByTitle 用 EnumWindows 按 GetWindowTextW 精确匹配标题，返回 hwnd（0 表示未找到）。
func findWindowByTitle(title string) uintptr {
	var found uintptr
	cb := windows.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if getWindowText(hwnd) == title {
			found = hwnd
			return 0 // 停止枚举
		}
		return 1 // 继续枚举
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// getWindowText 读取窗口标题（UTF-16）。
func getWindowText(hwnd uintptr) string {
	buf := make([]uint16, wmTitleMaxChars)
	n, _, _ := procGetWindowTextW.Call(hwnd,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return strings.TrimSpace(windows.UTF16ToString(buf[:n]))
}
