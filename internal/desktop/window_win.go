//go:build windows

package desktop

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
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
//
// Windows Terminal 存在时走 wt 分支：`wt -w new nt --title <title> -d <dir> powershell ...`；
// 否则用 `cmd /c start "<title>" powershell ...`（start 的首个带引号参数即窗口标题，
// 比依赖 $host.UI.RawUI.WindowTitle 更稳，后者在输出被重定向的场景会抛异常）。
//
// 命令一律经 -EncodedCommand 传递而非 -Command：Windows Terminal 会把命令行里的
// 分号当作自身子命令分隔符，导致 -Command 多语句只有第一条生效（实测：第二条及之后
// 全部丢失且窗口随即退出）。
//
// 工作目录三重保险：wt 侧用 -d <dir> 设起始目录，进程侧 cmd.Dir = dir 保证继承，
// 命令侧再 Set-Location 兜底，覆盖个别 wt 版本不继承启动器 cwd 的差异。
func (l *windowsLauncher) Launch(dir, title string, args []string) error {
	script := "Set-Location -LiteralPath " + psQuote(dir)
	for _, a := range args {
		script += "; " + a
	}
	encoded := psEncode(script)

	if wtPath := findWt(); wtPath != "" {
		cmd := exec.Command(wtPath, "-w", "new", "nt", "--title", title, "-d", dir,
			"powershell", "-NoExit", "-EncodedCommand", encoded)
		cmd.Dir = dir
		return cmd.Start()
	}
	// 回退路径额外在脚本内重设标题：PowerShell 启动时会把控制台标题改写为
	// "Windows PowerShell"，仅靠 cmd start 的首个带引号参数不足以保住标题，
	// 而标题是聚焦与存活检测的唯一键。RawUI 在输出被重定向时会抛异常，
	// 因此包在 try/catch 里，失败也只影响标题、不影响后续命令。
	fallbackScript := "try { $host.UI.RawUI.WindowTitle = " + psQuote(title) + " } catch { }; " + script
	cmd := exec.Command(os.Getenv("COMSPEC"), "/c", "start", title,
		"powershell", "-NoExit", "-EncodedCommand", psEncode(fallbackScript))
	cmd.Dir = dir
	return cmd.Start()
}

// psQuote 将文本包成 PowerShell 单引号字符串字面量（内嵌单引号双写转义）。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// psEncode 生成 PowerShell -EncodedCommand 所需的 UTF-16LE Base64 文本。
func psEncode(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
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
