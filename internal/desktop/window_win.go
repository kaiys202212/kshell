//go:build windows

package desktop

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/yangk/kshell/internal/executil"
)

func init() {
	defaultProcAlive = windowsProcAlive
	defaultLauncherFactory = func() TerminalLauncher { return &windowsLauncher{} }
}

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
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
	swRestore      = 9
	vkMenu         = 0x12 // ALT
	keyeventfKeyUp = 0x2
	maxTitleChars  = 1024
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
// 否则用 `cmd /S /C "start "<title>" powershell ..."`（start 的首个带引号参数即窗口标题，
// 比依赖 $host.UI.RawUI.WindowTitle 更稳，后者在输出被重定向的场景会抛异常）。
//
// 命令一律经 -EncodedCommand 传递而非 -Command：Windows Terminal 会把命令行里的
// 分号当作自身子命令分隔符，导致 -Command 多语句只有第一条生效（实测：第二条及之后
// 全部丢失且窗口随即退出）。
//
// 两个分支的脚本都以「重设窗口标题」开头：标题是聚焦与存活检测的唯一键，而
// PowerShell 启动过程会把控制台标题改写为 "Windows PowerShell"。实测 wt 的
// --title 在多数版本能保住，但脚本内再设一次作为双保险（RawUI 在输出被重定向时
// 会抛异常，包 try/catch，失败只影响标题、不影响后续命令）。
//
// 工作目录三重保险：wt 侧用 -d <dir> 设起始目录，进程侧 cmd.Dir = dir 保证继承，
// 命令侧再 Set-Location 兜底，覆盖个别 wt 版本不继承启动器 cwd 的差异。
func (l *windowsLauncher) Launch(dir, title string, args []string) error {
	script := "try { $host.UI.RawUI.WindowTitle = " + psQuote(title) + " } catch { }; " +
		"Set-Location -LiteralPath " + psQuote(dir)
	for _, a := range args {
		script += "; " + a
	}
	encoded := psEncode(script)

	var cmd *exec.Cmd
	var cmdLine string
	if wtPath := findWt(); wtPath != "" {
		cmd = exec.Command(wtPath, "-w", "new", "nt", "--title", title, "-d", dir,
			"powershell", "-NoExit", "-EncodedCommand", encoded)
	} else {
		// 无 wt 时用 cmd start：必须 /S /C + 手写 CmdLine。
		// 标题含空格时旧写法 `cmd /c start <title> ...` 经 ComposeCommandLine
		// 会与其它引号参数互相踩踏（同类根因见 executil.BatchCommandLine）。
		comspec := os.Getenv("COMSPEC")
		if strings.TrimSpace(comspec) == "" {
			comspec = "cmd.exe"
		}
		cmd = exec.Command(comspec)
		cmdLine = startPowershellCmdLine(comspec, title, encoded)
	}
	cmd.Dir = dir
	// cmd start 回退分支里 cmd.exe 自身会闪一个黑窗（GUI 壳无控制台可继承）；
	// CREATE_NO_WINDOW 只隐藏 cmd 自己，start 创建的 powershell 新窗口不受影响。
	// wt 分支 wt.exe 是 GUI 程序，设置与否无区别，统一走辅助函数。
	executil.HideWindow(cmd)
	if cmdLine != "" {
		// HideWindow 会重建 SysProcAttr，须在其后写回 CmdLine。
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.CmdLine = cmdLine
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// fire-and-forget：不等待退出，立即释放进程句柄避免长驻泄漏
	return cmd.Process.Release()
}

// startPowershellCmdLine 构造无 wt 时的 cmd start 命令行（配合 SysProcAttr.CmdLine）。
// start 的窗口标题必须始终带引号（即使无空格），否则首个 token 可能被当成要运行的命令。
func startPowershellCmdLine(comspec, title, encoded string) string {
	titled := `"` + strings.ReplaceAll(title, `"`, `""`) + `"`
	inner := "start " + titled + " powershell -NoExit -EncodedCommand " + encoded
	return comspec + ` /S /C "` + inner + `"`
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

// enumCtx 是一次枚举调用的上下文。
// 回调经 lparam 收到一个自增 ID，从 enumCtxs 注册表取上下文——
// 避免 uintptr(unsafe.Pointer) 传栈指针（vet 报 possible misuse），也天然无竞争。
type enumCtx struct {
	title string
	found uintptr
}

// enumWndProc 是 EnumWindows 的回调。注意：syscall 回调在进程内有数量上限
// （约 2000 个），必须包级单例只创建一次——
// 每次调用 NewCallback 的写法会让长驻进程在若干小时后 panic 崩溃。
var (
	enumWndProc = windows.NewCallback(enumWndProcImpl)
	enumMu      sync.Mutex
	enumCtxs    = map[uintptr]*enumCtx{}
	enumNextID  uintptr
)

func enumWndProcImpl(hwnd, lparam uintptr) uintptr {
	enumMu.Lock()
	ctx := enumCtxs[lparam]
	enumMu.Unlock()
	if ctx == nil {
		return 1
	}
	if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
		return 1 // 跳过不可见窗口，避免匹配到其他应用的隐藏窗口
	}
	if getWindowText(hwnd) == ctx.title {
		ctx.found = hwnd
		return 0 // 停止枚举
	}
	return 1 // 继续枚举
}

// findWindowByTitle 用 EnumWindows 按 GetWindowTextW 精确匹配标题，返回 hwnd（0 表示未找到）。
func findWindowByTitle(title string) uintptr {
	enumMu.Lock()
	enumNextID++
	id := enumNextID
	ctx := &enumCtx{title: title}
	enumCtxs[id] = ctx
	enumMu.Unlock()

	procEnumWindows.Call(enumWndProc, id)

	enumMu.Lock()
	delete(enumCtxs, id)
	enumMu.Unlock()
	return ctx.found
}

// getWindowText 读取窗口标题（UTF-16）。
func getWindowText(hwnd uintptr) string {
	buf := make([]uint16, maxTitleChars)
	n, _, _ := procGetWindowTextW.Call(hwnd,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return strings.TrimSpace(windows.UTF16ToString(buf[:n]))
}
