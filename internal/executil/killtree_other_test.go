//go:build !windows

package executil

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestKillProcessTreeKillsGrandchildOther 复现 mac 上 npx 兜底的孤儿机制：
// 直属进程 spawn 孙进程；组杀必须连孙进程一并清掉。
func TestKillProcessTreeKillsGrandchildOther(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅非 Windows")
	}
	pidFile := t.TempDir() + "/child.pid"
	// 子进程记录自身 PID 后拉起孙进程 sleep 30 并 wait，构成两层树。
	script := `echo $$ > ` + pidFile + `; sleep 30 & wait`
	cmd := exec.Command("/bin/sh", "-c", script)
	PrepareProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer KillProcessTree(cmd)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("等待子进程退出失败: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // 给孙进程收尸留窗口
	// 用 PID 精确验证孙进程（脚本的 sleep 子进程）已死：kill -0 报错即已退出。
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("读取子进程 PID 失败: %v", err)
	}
	pid := strings.TrimSpace(string(b))
	if err := exec.Command("kill", "-0", pid).Run(); err == nil {
		t.Fatalf("孙进程 %s 仍存活，树杀未生效", pid)
	}
}

// TestKillProcessTreeOtherNilSafe 空指针/未启动进程不得 panic。
func TestKillProcessTreeOtherNilSafe(t *testing.T) {
	KillProcessTree(nil)
	KillProcessTree(&exec.Cmd{})
}

// TestPrepareProcessGroupSetsPgid 未设 SysProcAttr 时应自动创建并置位；已有时不覆盖。
func TestPrepareProcessGroupSetsPgid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅非 Windows")
	}
	cmd := exec.Command("true")
	PrepareProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid 未置位")
	}
	existing := &syscall.SysProcAttr{Foreground: true}
	cmd2 := exec.Command("true")
	cmd2.SysProcAttr = existing
	PrepareProcessGroup(cmd2)
	if cmd2.SysProcAttr != existing || !existing.Setpgid {
		t.Fatal("既有 SysProcAttr 被覆盖或未置位")
	}
}
