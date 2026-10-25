//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/yangk/kshell/internal/executil"
	"golang.org/x/sys/windows"
)

// desktopExeName 抽象「当前 exe 名」来源（包级变量便于测试替换）。peer 枚举
// 必须按实际安装名过滤：exe 改名安装（如 kshell-desktop1.exe）时若硬编码
// kshell-desktop.exe，残留实例/助手清不掉，新进程会被单实例锁挡住静默退出。
var desktopExeName = func() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// 拿不到自身路径时回退传统安装名，聊胜于无。
		return "kshell-desktop.exe"
	}
	return filepath.Base(exe)
}

// wqlLiteral 转义 WQL 单引号字符串字面量内的单引号（WQL 无反斜杠转义，
// 成对单引号即字面单引号）。
func wqlLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// helperCmdMarkers 已知助手子进程的命令行标记：mcp-archive / mcp-ssh（agent
// 会话的 MCP 服务）、agent-hook、SSH askpass。它们由 agent CLI 会话持有，
// GUI 退出后继续存活。
var helperCmdMarkers = []string{"mcp-", "agent-hook", "--ssh-askpass"}

// isGuiPeer 按命令行形态判定同 exe 名进程是否 GUI 实例。升级清场只清 GUI：
// 单实例互斥锁只有 GUI 持有，改名交换后助手进程已不阻塞任何替换步骤，杀
// 它们纯属破坏在跑的 agent 会话；助手死活只影响残留（.upgrade-old*）补删
// 的时点，残留由下次升级脚本兜底。判定按序：
//  1. CommandLine 为空（权限不足/已退出取不到）：保守视为 GUI，否则残留
//     GUI 实例永远清不掉、新进程会被单实例锁挡住静默退出；
//  2. 去引号后仅 exe 路径本身（无任何参数）：GUI；
//  3. 含 --after-update（升级拉起的新 GUI）：GUI；
//  4. 含已知助手标记或带任意其他参数：一律不杀（保守：拿不准的宁可留着，
//     宁可残留删不掉也不能误杀在跑会话）。
func isGuiPeer(cmdline, exeName string) bool {
	c := strings.TrimSpace(cmdline)
	if c == "" {
		return true
	}
	// 纯路径判定：去掉包裹引号后整体（或其末段）即 exe 名，且后面没有参数。
	// 用末段后缀匹配而非按空格切 token，兼容未加引号、含空格的安装路径。
	u := strings.TrimSpace(strings.Trim(c, `"`))
	ln := strings.ToLower(exeName)
	lu := strings.ToLower(u)
	if lu == ln || strings.HasSuffix(lu, `\`+ln) {
		return true
	}
	if strings.Contains(c, afterUpdateFlag) {
		return true
	}
	for _, m := range helperCmdMarkers {
		if strings.Contains(c, m) {
			return false
		}
	}
	return false
}

// desktopPeerFilterCmd 构造枚举同映像 peer 进程的 PowerShell 命令；除 PID 外
// 一并取 CommandLine 供 isGuiPeer 白名单判定。单独拆出是为了测试能校验过滤
// 值确实来自实际 exe 名而非硬编码。输出为 "PID cmdline" 行（PID 后第一个空
// 格分隔，CommandLine 为空时仅 PID）；不用默认表格式输出，因其按控制台宽度
// 截断长命令行。
func desktopPeerFilterCmd() *exec.Cmd {
	return exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`Get-CimInstance Win32_Process -Filter "Name='%s'" | Select-Object ProcessId,CommandLine | ForEach-Object { '{0} {1}' -f $_.ProcessId, $_.CommandLine }`,
			wqlLiteral(desktopExeName())))
}

// parsePeerOutput 解析 desktopPeerFilterCmd 的 "PID cmdline" 行输出，剔除
// 自身与非 GUI（助手/未知参数）进程，返回参与升级清场的 GUI peer PID。
func parsePeerOutput(out []byte, self int, exeName string) []int {
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pidStr, cmdline := line, ""
		if i := strings.Index(line, " "); i >= 0 {
			pidStr, cmdline = line[:i], line[i+1:]
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == self {
			continue
		}
		if !isGuiPeer(cmdline, exeName) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func listDesktopPeerPIDs() ([]int, error) {
	cmd := desktopPeerFilterCmd()
	executil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parsePeerOutput(out, os.Getpid(), desktopExeName()), nil
}

func singleInstanceMutexFree() bool {
	name, err := syscall.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return true
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if handle != 0 {
		_ = windows.CloseHandle(handle)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return false
	}
	return true
}

func killProcessIDs(pids []int) error {
	var last error
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
		executil.HideWindow(cmd)
		if err := cmd.Run(); err != nil {
			last = fmt.Errorf("taskkill %d: %w", pid, err)
		}
	}
	return last
}
