package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// writeRawFile 原样写文件（不加 shim 包装、不改后缀），用于 node.exe / index.js 这类精确命名的入口。
func writeRawFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDetectNodeEntryFallback(t *testing.T) {
	home := t.TempDir()
	vdir := filepath.Join(home, "va")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 同目录 node.exe 与 index.js 存活，但无任何 cursor-agent/agent shim
	writeRawFile(t, filepath.Join(vdir, "node.exe"))
	writeRawFile(t, filepath.Join(vdir, "index.js"))
	t.Setenv("PATH", t.TempDir())
	spec := DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{vdir},
		ConfigDirs:      []string{filepath.Join(home, ".cursor")},
		NodeEntryScript: "index.js",
	}
	got := Detect(spec, home)
	if !got.Installed || got.Source != "node-entry" {
		t.Fatalf("应命中 node-entry 兜底, got %+v", got)
	}
	if want := filepath.Join(vdir, "node.exe"); filepath.Clean(got.BinPath) != filepath.Clean(want) {
		t.Fatalf("BinPath = %q, want %q", got.BinPath, want)
	}
	// 主脚本必须是绝对路径：启动会话时子进程 cwd 是工作区，相对路径会解析错
	if len(got.BinArgs) != 1 || filepath.Clean(got.BinArgs[0]) != filepath.Clean(filepath.Join(vdir, "index.js")) {
		t.Fatalf("BinArgs = %v, want [%s]", got.BinArgs, filepath.Join(vdir, "index.js"))
	}
}

func TestDetectPrefersShimOverNodeEntry(t *testing.T) {
	home := t.TempDir()
	vdir := filepath.Join(home, "va")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, filepath.Join(vdir, "node.exe"))
	writeRawFile(t, filepath.Join(vdir, "index.js"))
	writeFakeBin(t, vdir, "cursor-agent", "echo shim") // shim 仍在
	t.Setenv("PATH", t.TempDir())
	spec := DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{vdir},
		NodeEntryScript: "index.js",
	}
	got := Detect(spec, home)
	if !got.Installed {
		t.Fatalf("应检测到已安装, got %+v", got)
	}
	if got.Source == "node-entry" {
		t.Fatal("shim 存在时不得走 node-entry 兜底")
	}
}

func TestDetectNodeEntryRequiresScript(t *testing.T) {
	home := t.TempDir()
	vdir := filepath.Join(home, "va")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, filepath.Join(vdir, "node.exe")) // 缺 index.js
	t.Setenv("PATH", t.TempDir())
	spec := DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{vdir},
		NodeEntryScript: "index.js",
	}
	got := Detect(spec, home)
	if got.Installed && got.Source == "node-entry" {
		t.Fatal("缺主脚本时不得命中 node-entry 兜底")
	}
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

func TestProbeVersionAppendsVersionFlagAfterArgs(t *testing.T) {
	dir := t.TempDir()
	var script string
	if runtime.GOOS == "windows" {
		script = filepath.Join(dir, "probe.cmd")
		if err := os.WriteFile(script, []byte("@echo off\r\necho out %*\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script = filepath.Join(dir, "probe")
		if err := os.WriteFile(script, []byte("#!/bin/sh\necho out \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := ProbeVersion(script, "index.js"); !strings.HasPrefix(got, "out index.js --version") {
		t.Fatalf("参数应在 --version 之前透传, got %q", got)
	}
}
