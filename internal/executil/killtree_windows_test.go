//go:build windows

package executil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestKillProcessTreeKillsGrandchild 复现 npm .cmd 包装的孤儿机制：
// exec 起批处理（cmd.exe）→ 批处理内运行 ping（孙代）。只 Process.Kill 直属
// cmd.exe 时 ping 会成孤儿存活；KillProcessTree 必须整树清掉。
func TestKillProcessTreeKillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	bat := filepath.Join(dir, "tree-kill-probe.cmd")
	script := "@echo off\r\nping -n 30 127.0.0.1 > nul\r\n"
	if err := os.WriteFile(bat, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bat)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 .cmd 失败: %v", err)
	}

	// 等孙代出现：cmd.exe 解析批处理后才会 CreateProcess 拉起 ping。
	childPID, err := waitGrandchild(t, cmd.Process.Pid, 10*time.Second)
	if err != nil {
		t.Fatalf("未观察到 ping 子进程: %v", err)
	}

	KillProcessTree(cmd)
	// Wait 返回即证直属进程已退出；被强杀返回 ExitError 属预期，不校验错误值。
	_ = cmd.Wait()

	// 直属进程必须已退出（Wait 返回即证）。
	// 孙代必须消失：轮询确认，给系统几秒收敛窗口。
	deadline := time.Now().Add(10 * time.Second)
	for {
		if !processExists(childPID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("孙进程 ping (pid=%d) 在树杀后仍存活：孤儿泄漏复现", childPID)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestKillProcessTreeNilSafe 空指针/未启动进程不得 panic。
func TestKillProcessTreeNilSafe(t *testing.T) {
	KillProcessTree(nil)
	KillProcessTree(&exec.Cmd{})
}

// waitGrandchild 轮询等待指定进程的第一个子进程出现，返回其 PID。
func waitGrandchild(t *testing.T, parentPID int, timeout time.Duration) (int, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		pid, err := firstChildPID(parentPID)
		if err == nil {
			return pid, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("等待超时: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// firstChildPID 经 PowerShell 查询直属子进程（本仓库 Windows 测试环境有 PowerShell）。
func firstChildPID(parentPID int) (int, error) {
	ps := fmt.Sprintf(
		`(Get-CimInstance Win32_Process -Filter "ParentProcessId=%d" | Select-Object -First 1).ProcessId`,
		parentPID)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(out), "%d", &pid); err != nil {
		return 0, fmt.Errorf("无子进程: %w", err)
	}
	return pid, nil
}

// processExists 报告进程是否仍存活。
func processExists(pid int) bool {
	ps := fmt.Sprintf(`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { 'yes' }`, pid)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return false
	}
	return len(out) > 0 && out[0] == 'y'
}
