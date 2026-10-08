//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/yangk/kshell/internal/executil"
	"golang.org/x/sys/windows"
)

func listDesktopPeerPIDs() ([]int, error) {
	self := os.Getpid()
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_Process -Filter "Name='kshell-desktop.exe'" | Select-Object -ExpandProperty ProcessId`)
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
