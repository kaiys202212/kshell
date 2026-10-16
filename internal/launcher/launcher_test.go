package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

// 一段跨平台的“可退出”脚本（Build/Resolve 链路的夹具）。
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
	} else if err.Error() != "err.launcher.no_exec" {
		t.Fatalf("error = %q, want err.launcher.no_exec", err.Error())
	}
}
