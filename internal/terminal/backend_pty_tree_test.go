package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// pidAlive 报告指定 PID 的进程是否仍在运行。
func pidAlive(t *testing.T, pid int) bool {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
		if err != nil {
			t.Fatalf("tasklist: %v", err)
		}
		return strings.Contains(string(out), strconv.Itoa(pid))
	}
	out, err := exec.Command("kill", "-0", strconv.Itoa(pid)).CombinedOutput()
	return err == nil && !strings.Contains(string(out), "no such process")
}

// TestPTYCloseKillsDetachedGrandchildren 覆盖孤儿进程根因。
// 同一控制台内的孙进程会随 ConPTY 关闭被 CTRL_CLOSE 连带终止（Windows 控制台语义），
// 但 shell 里经 `start` 拉起的新控制台进程不受 ConPTY 管——只 Process.Kill() 直接
// 子进程时它会残留成孤儿。关闭页签必须按父子关系整树终止（taskkill /T）。
// 结构：PTY 直属 cmd.exe 跑 parent.cmd → `start` 拉起 powershell（新控制台，
// 写下自己的 PID 后长眠 60s）。
func TestPTYCloseKillsDetachedGrandchildren(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("孙进程树终止验证在 Windows（ConPTY）真机跑")
	}
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("本机 PATH 上没有 powershell")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	parent := filepath.Join(dir, "parent.cmd")
	// start 开新控制台（脱离 ConPTY）；孙进程写 PID 后睡 60s：若 Close 不杀树，它一定活着。
	// 路径无空格（TempDir），双引号包路径即可。
	body := "@echo off\r\n" +
		"start \"\" powershell -NoProfile -Command \"$PID | Set-Content -Path \\\"" + pidFile + "\\\"; Start-Sleep -Seconds 60\"\r\n" +
		"ping -n 11 127.0.0.1 >nul\r\n"
	if err := os.WriteFile(parent, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h, err := NewPTYBackend().Start(Spec{Path: parent, Dir: dir}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// 等孙进程把 PID 写出来（写完后文件句柄可能延迟关闭，读时重试）
	var raw []byte
	waitFor(t, "grandchild pid file readable", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		raw = b
		return true
	})
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("pid file = %q", raw)
	}
	waitFor(t, "grandchild visible in tasklist", func() bool { return pidAlive(t, pid) })

	// 关闭页签：整棵进程树（含脱离 ConPTY 的新控制台孙进程）都应终止
	_ = h.Close()
	waitFor(t, "grandchild to die after Close", func() bool { return !pidAlive(t, pid) })
}
