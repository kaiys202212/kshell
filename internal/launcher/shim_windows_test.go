//go:build windows

package launcher

import (
	"os"
	"path/filepath"
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

	spec := Resolve(filepath.Join(dir, "codex.ps1"), []string{"resume", "abc"})
	if spec.Path != cmdPath {
		t.Fatalf("path = %q, want the .cmd sibling %q", spec.Path, cmdPath)
	}
	if len(spec.Args) != 2 || spec.Args[0] != "resume" {
		t.Fatalf("args = %v, want them preserved", spec.Args)
	}
}

func TestResolveShimWrapsPs1(t *testing.T) {
	dir := t.TempDir()
	ps1 := filepath.Join(dir, "codex.ps1")
	if err := os.WriteFile(ps1, []byte("# ps1"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := Resolve(ps1, []string{"resume", "abc"})
	if spec.Path != "powershell" {
		t.Fatalf("path = %q, want powershell", spec.Path)
	}
	if len(spec.Args) != 3 || spec.Args[0] != "-NoProfile" || spec.Args[1] != "-Command" {
		t.Fatalf("args = %v", spec.Args)
	}
	// 参数必须嵌进 -Command 脚本里，否则 PowerShell 会把它们当成自己的参数。
	if !contains(spec.Args[2], "resume") || !contains(spec.Args[2], "abc") {
		t.Fatalf("command script missing args: %q", spec.Args[2])
	}
	if !contains(spec.Args[2], "codex.ps1") {
		t.Fatalf("command script missing script path: %q", spec.Args[2])
	}
}

func TestResolveShimQuotesPathsWithSpaces(t *testing.T) {
	dir := t.TempDir()
	ps1 := filepath.Join(dir, "my tool.ps1")
	if err := os.WriteFile(ps1, []byte("# ps1"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := Resolve(ps1, nil)
	if !contains(spec.Args[2], "'") {
		t.Fatalf("path with spaces must be single quoted: %q", spec.Args[2])
	}
}

func TestResolveLeavesExeAlone(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := Resolve(exe, []string{"--resume", "x"})
	if spec.Path != exe || len(spec.Args) != 2 {
		t.Fatalf("spec = %+v, want a plain passthrough", spec)
	}
}
