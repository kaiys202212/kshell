//go:build windows

package executil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveShimPrefersCmdSibling(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex.ps1"), []byte("# ps1"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdPath := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(cmdPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, args := ResolveShim(filepath.Join(dir, "codex.ps1"), []string{"resume", "abc"})
	if path != cmdPath {
		t.Fatalf("path = %q, want the .cmd sibling %q", path, cmdPath)
	}
	if len(args) != 2 || args[0] != "resume" {
		t.Fatalf("args = %v, want them preserved", args)
	}
}

func TestResolveShimWrapsPs1(t *testing.T) {
	dir := t.TempDir()
	ps1 := filepath.Join(dir, "codex.ps1")
	if err := os.WriteFile(ps1, []byte("# ps1"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, args := ResolveShim(ps1, []string{"resume", "abc"})
	if path != "powershell" {
		t.Fatalf("path = %q, want powershell", path)
	}
	if len(args) != 3 || args[0] != "-NoProfile" || args[1] != "-Command" {
		t.Fatalf("args = %v", args)
	}
	// 参数必须嵌进 -Command 脚本里，否则 PowerShell 会把它们当成自己的参数。
	if !strings.Contains(args[2], "resume") || !strings.Contains(args[2], "abc") {
		t.Fatalf("command script missing args: %q", args[2])
	}
	if !strings.Contains(args[2], "codex.ps1") {
		t.Fatalf("command script missing script path: %q", args[2])
	}
}

func TestResolveShimQuotesPathsWithSpaces(t *testing.T) {
	dir := t.TempDir()
	ps1 := filepath.Join(dir, "my tool.ps1")
	if err := os.WriteFile(ps1, []byte("# ps1"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, args := ResolveShim(ps1, nil)
	if !strings.Contains(args[2], "'") {
		t.Fatalf("path with spaces must be single quoted: %q", args[2])
	}
}

func TestResolveShimLeavesExeAlone(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, args := ResolveShim(exe, []string{"--resume", "x"})
	if path != exe || len(args) != 2 {
		t.Fatalf("path = %q, args = %v, want a plain passthrough", path, args)
	}
}
