//go:build windows

package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProbeVersionSpacedCmdPath：含空格的 .cmd + 需引号的入口参数曾静默返回 unknown
// （cmd.exe 把 argv0 在空格处截断；入口参数是 node 主脚本路径等含空格值时触发）。
func TestProbeVersionSpacedCmdPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "claude.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\necho 9.9.9\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProbeVersion(bat, `C:\Program Files Fake\main.js`); got != "9.9.9" {
		t.Fatalf("ProbeVersion = %q, want 9.9.9", got)
	}
}
