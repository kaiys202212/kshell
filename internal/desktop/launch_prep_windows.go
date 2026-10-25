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

// desktopPeerFilterCmd 构造枚举同映像 peer 进程的 PowerShell 命令；单独拆出
// 是为了测试能校验过滤值确实来自实际 exe 名而非硬编码。
func desktopPeerFilterCmd() *exec.Cmd {
	return exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`Get-CimInstance Win32_Process -Filter "Name='%s'" | Select-Object -ExpandProperty ProcessId`,
			wqlLiteral(desktopExeName())))
}

func listDesktopPeerPIDs() ([]int, error) {
	self := os.Getpid()
	cmd := desktopPeerFilterCmd()
	executil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid == self {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
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
