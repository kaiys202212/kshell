//go:build !windows

package executil

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestKillProcessTreeKillsGrandchildOther 复现 mac 上 npx 兜底的孤儿机制：
// 直属进程（sh）spawn 孙进程（sleep），KillProcessTree 组杀必须连孙进程一并清掉。
func TestKillProcessTreeKillsGrandchildOther(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅非 Windows")
	}
	pidFile := t.TempDir() + "/grandchild.pid"
	// 孙进程记录自身 PID（$!，后台 job 的真身）后长期休眠，构成 sh → sleep 两层树。
	script := `sleep 30 & echo $! > ` + pidFile + `; wait`
	cmd := exec.Command("/bin/sh", "-c", script)
	PrepareProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer KillProcessTree(cmd) // 断言失败也不留孤儿

	// 轮询等孙进程 PID 落盘（文件由直属子进程 sh 写入）。
	var grandchild string
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(pidFile)
		if err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				grandchild = v
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("孙进程 PID 未落盘")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 主动组杀（核心验证点）：若组杀失效，sh 死后 sleep 会成孤儿存活。
	KillProcessTree(cmd)
	_ = cmd.Wait() // sh 被信号杀死，err 属预期

	// 组杀后孙进程必须退出：kill(pid, 0) 返回 ESRCH 即已死（进程内探活，
	// 不依赖外部 kill 命令）。
	gpid, err := strconv.Atoi(grandchild)
	if err != nil {
		t.Fatalf("孙进程 PID 非法: %q", grandchild)
	}
	if err := syscall.Kill(gpid, syscall.Signal(0)); err == nil {
		t.Fatalf("孙进程 %d 仍存活，树杀未生效", gpid)
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
	existing := &syscall.SysProcAttr{}
	cmd2 := exec.Command("true")
	cmd2.SysProcAttr = existing
	PrepareProcessGroup(cmd2)
	if cmd2.SysProcAttr != existing || !existing.Setpgid {
		t.Fatal("既有 SysProcAttr 被覆盖或未置位")
	}
}
