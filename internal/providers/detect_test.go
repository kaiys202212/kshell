package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeBin(t *testing.T, dir, name, body string) string {
	t.Helper()
	var path string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".cmd")
		body = "@echo off\r\n" + body + "\r\n"
	} else {
		path = filepath.Join(dir, name)
		body = "#!/bin/sh\n" + body + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
	return path
}

func TestDetectFindsBinOnPath(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir, "claude", "echo claude 2.1.81")
	t.Setenv("PATH", dir)

	got := Detect(DetectSpec{BinName: "claude"}, t.TempDir())
	if !got.Installed {
		t.Fatalf("expected installed, got %+v", got)
	}
	if got.Source != "path" {
		t.Fatalf("source = %q, want path", got.Source)
	}
	if filepath.Clean(got.BinPath) != filepath.Clean(bin) {
		t.Fatalf("bin = %q, want %q", got.BinPath, bin)
	}
}

func TestDetectFallsBackToInstallDir(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeBin(t, dir, "claude", "echo claude 2.1.81")
	t.Setenv("PATH", t.TempDir()) // PATH 上没有

	got := Detect(DetectSpec{BinName: "claude", InstallDirs: []string{"~/.claude/local"}}, home)
	if !got.Installed || got.Source != "install-dir" {
		t.Fatalf("got %+v, want installed via install-dir", got)
	}
}

func TestDetectFallsBackToConfigDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	got := Detect(DetectSpec{BinName: "claude", ConfigDirs: []string{"~/.claude"}}, home)
	if !got.Installed || got.Source != "config-dir" {
		t.Fatalf("got %+v, want installed via config-dir", got)
	}
	if got.BinPath != "" {
		t.Fatalf("config-dir detection must not invent a bin path, got %q", got.BinPath)
	}
}

func TestDetectMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := Detect(DetectSpec{BinName: "definitely-not-installed", ConfigDirs: []string{"~/.nope"}}, t.TempDir())
	if got.Installed {
		t.Fatalf("got %+v, want not installed", got)
	}
}

func TestProbeVersion(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir, "claude", "echo claude 2.1.81")
	t.Setenv("PATH", dir)

	if got := ProbeVersion(bin); got != "claude 2.1.81" {
		t.Fatalf("version = %q, want %q", got, "claude 2.1.81")
	}
}

func TestFindBinsReturnsPathAndInstallDir(t *testing.T) {
	home := t.TempDir()
	pathDir := t.TempDir()
	pathBin := writeFakeBin(t, pathDir, "claude", "echo path")
	t.Setenv("PATH", pathDir)

	installDir := filepath.Join(home, ".claude", "local")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installBin := writeFakeBin(t, installDir, "claude", "echo local")

	got := FindBins(DetectSpec{BinName: "claude", InstallDirs: []string{"~/.claude/local"}}, home)
	want := map[string]bool{filepath.Clean(pathBin): true, filepath.Clean(installBin): true}
	if len(got) != 2 {
		t.Fatalf("FindBins = %v, want 2 paths", got)
	}
	for _, p := range got {
		if !want[filepath.Clean(p)] {
			t.Fatalf("unexpected bin %q in %v", p, got)
		}
	}
}

func TestFindBinsIgnoresConfigDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	got := FindBins(DetectSpec{BinName: "claude", ConfigDirs: []string{"~/.claude"}}, home)
	if len(got) != 0 {
		t.Fatalf("config-only must not list bins, got %v", got)
	}
}

func TestProbeVersionUnknownWhenCommandFails(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeBin(t, dir, "broken", "exit 1")
	t.Setenv("PATH", dir)

	if got := ProbeVersion(bin); got != "unknown" {
		t.Fatalf("version = %q, want unknown", got)
	}
}
