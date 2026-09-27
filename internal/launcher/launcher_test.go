package launcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// 一段跨平台的“可退出”脚本，用来验证 Run 的输出与退出码捕获。
func exitScript(t *testing.T, dir, name string, code int, msg string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return writeScript(t, dir, name+".cmd", "@echo off\r\necho "+msg+"\r\nexit /b "+itoa(code)+"\r\n")
	}
	return writeScript(t, dir, name, "#!/bin/sh\necho "+msg+"\nexit "+itoa(code)+"\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestBuildPassesThroughExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := exitScript(t, dir, "tool", 0, "hi")

	spec, err := Build(providers.Launch{Path: bin, Args: []string{"resume", "abc"}, Dir: dir})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if spec.Path != bin {
		t.Fatalf("path = %q, want %q", spec.Path, bin)
	}
	if len(spec.Args) != 2 || spec.Args[0] != "resume" || spec.Args[1] != "abc" {
		t.Fatalf("args = %v", spec.Args)
	}
	if spec.Dir != dir {
		t.Fatalf("dir = %q", spec.Dir)
	}
}

func TestBuildRejectsEmptyPath(t *testing.T) {
	if _, err := Build(providers.Launch{Args: []string{"x"}}); err == nil {
		t.Fatal("empty binary path must be rejected")
	}
}

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	dir := t.TempDir()
	bin := exitScript(t, dir, "tool", 3, "boom")

	spec, err := Build(providers.Launch{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
	if !contains(res.Stdout, "boom") {
		t.Fatalf("stdout = %q, want it to contain boom", res.Stdout)
	}
}

func TestRunTimeoutKillsProcess(t *testing.T) {
	dir := t.TempDir()
	var bin string
	if runtime.GOOS == "windows" {
		// 用 powershell 的 Start-Sleep：杀得掉；ping 这类会派生子进程，父进程被杀子进程仍在。
		bin = writeScript(t, dir, "sleep.cmd", "@echo off\r\npowershell -NoProfile -Command Start-Sleep -Seconds 10\r\n")
	} else {
		bin = writeScript(t, dir, "sleep", "#!/bin/sh\nsleep 10\n")
	}

	spec, err := Build(providers.Launch{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := Run(ctx, spec)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("run was not cancelled, elapsed %s", elapsed)
	}
	// 被取消的进程要么返回错误，要么给出非 0 退出码；绝不能看起来像正常结束。
	if err == nil && res.ExitCode == 0 {
		t.Fatal("cancelled run must not look like a successful one")
	}
}

func TestTailReturnsLastLines(t *testing.T) {
	res := Result{Stdout: "l1\nl2\nl3\nl4"}
	got := res.Tail(2)
	if !contains(got, "l3") || !contains(got, "l4") {
		t.Fatalf("tail = %q", got)
	}
	if contains(got, "l1") {
		t.Fatalf("tail should drop earlier lines: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && index(haystack, needle) >= 0)
}

func index(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
