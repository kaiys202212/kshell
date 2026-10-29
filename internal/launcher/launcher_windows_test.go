//go:build windows

package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpecCmdRunsSpacedCmdWithArgs：TUI 恢复/新建会话时，CLI 位于含空格目录也要能启动。
func TestSpecCmdRunsSpacedCmdWithArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "opencode.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\necho ARGS:[%*]\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 含空格的参数（如工作区 / 主脚本路径）才会触发 argv0 截断。
	spec := Spec{Path: bat, Args: []string{"--cwd", `C:\Program Files\wk`}}
	out, err := spec.Cmd(context.Background()).Output()
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != `ARGS:[--cwd "C:\Program Files\wk"]` {
		t.Fatalf("参数未完整送达: %q", got)
	}
}
