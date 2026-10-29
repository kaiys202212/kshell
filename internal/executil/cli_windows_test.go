//go:build windows

package executil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewCLICommandRunsSpacedCmdWithSpacedArgs 复现机器 B 的报错：
// 含空格的 .cmd + 含空格的参数，必须经 cmd.exe 包装后完整送达。
func TestNewCLICommandRunsSpacedCmdWithSpacedArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "tool.cmd")
	body := "@echo off\r\necho ARGS:[%*]\r\necho SQL:[%~2]\r\nexit /b 0\r\n"
	if err := os.WriteFile(bat, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	sql := `select id, directory as cwd from session where parent_id is null order by time_updated desc`
	cmd := NewCLICommand(context.Background(), bat, []string{"db", sql, "--format", "json"})
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("NewCLICommand 执行失败: %v", err)
	}
	want := "ARGS:[db \"" + sql + "\" --format json]\r\nSQL:[" + sql + "]"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("参数未完整送达: %q, want %q", got, want)
	}
}

// TestWrapBatchFileLeavesExeAlone 非批处理保持原样，不应写 CmdLine。
func TestWrapBatchFileLeavesExeAlone(t *testing.T) {
	cmd := NewCLICommand(context.Background(), "cmd.exe", []string{"/c", "echo hi"})
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		t.Fatalf("非批处理不应写 CmdLine: %q", cmd.SysProcAttr.CmdLine)
	}
}

// TestWrapBatchFileKeepsHiddenWindow 锁死调用顺序：HideWindow 的标志不能被包装抹掉。
func TestWrapBatchFileKeepsHiddenWindow(t *testing.T) {
	bat := filepath.Join(t.TempDir(), "Program Files Fake", "tool.cmd")
	cmd := NewCLICommand(context.Background(), bat, []string{"a b"})
	attr := cmd.SysProcAttr
	if attr == nil || !attr.HideWindow || attr.CreationFlags != createNoWindow || attr.CmdLine == "" {
		t.Fatalf("批处理包装后应保留隐藏窗口标志: %+v", attr)
	}
}
