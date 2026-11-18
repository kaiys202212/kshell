//go:build windows

package desktop

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRestartWaitStartPowerShell(t *testing.T) {
	pid := 12345
	exe := `C:\Program Files\kshell\app.exe`
	ps := restartWaitStartPowerShell(pid, exe)
	if !strings.Contains(ps, fmt.Sprintf("Wait-Process -Id %d", pid)) {
		t.Fatalf("应等待 PID %d: %q", pid, ps)
	}
	if !strings.Contains(ps, "C:\\Program Files\\kshell\\app.exe") {
		t.Fatalf("应包含 LiteralPath 路径: %q", ps)
	}
	if !strings.Contains(ps, "Start-Sleep") {
		t.Fatalf("应等待单实例锁释放: %q", ps)
	}
	if !strings.Contains(ps, "-WindowStyle Normal") {
		t.Fatalf("新实例窗口应 Normal: %q", ps)
	}
	if !strings.Contains(ps, "-ArgumentList '--after-update'") {
		t.Fatalf("应带升级/重启标记: %q", ps)
	}
	// PS 5.1 的 Start-Process 没有 -LiteralPath，用了会直接报错导致重启后无进程。
	if !strings.Contains(ps, "Start-Process -FilePath") || strings.Contains(ps, "-LiteralPath") {
		t.Fatalf("Start-Process 必须用 -FilePath（PS 5.1）: %q", ps)
	}

	pid = os.Getpid()
	ps = restartWaitStartPowerShell(pid, exe)
	if !strings.Contains(ps, fmt.Sprintf("Wait-Process -Id %d", pid)) {
		t.Fatalf("应包含当前 PID %d: %q", pid, ps)
	}
}

func TestSpawnSelfDelayedCmd_脱离父进程(t *testing.T) {
	cmd := spawnSelfDelayedCmd(`C:\kshell\kshell-desktop.exe`)
	if cmd.SysProcAttr == nil {
		t.Fatal("未脱离父进程 Job")
	}
	if cmd.SysProcAttr.CreationFlags&0x01000000 == 0 {
		t.Fatalf("CreationFlags = %#x 缺少 CREATE_BREAKAWAY_FROM_JOB", cmd.SysProcAttr.CreationFlags)
	}
}
