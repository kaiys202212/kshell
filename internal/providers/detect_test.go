package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// agePath 把路径 mtime 拨回到 age 之前，用于模拟"陈旧"状态。
func agePath(t *testing.T, path string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// updatingSpec 构造带 node-entry 兜底与 config-dir 的检测规格（cursor 形态）。
func updatingSpec(home, installDir string) DetectSpec {
	return DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{installDir},
		ConfigDirs:      []string{"~/.cursor"},
		NodeEntryScript: "index.js",
	}
}

// 更新器下载 zip 待解压形态：versions\ 下有新鲜 zip、无任何可执行入口 → 更新中。
func TestDetectUpdatingByRecentZip(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cursor-agent")
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 目录整体拨旧，隔离出「仅 zip 新鲜」这一个信号
	agePath(t, root, 5*time.Minute)
	agePath(t, filepath.Join(root, "versions"), 5*time.Minute)
	writeRawFile(t, filepath.Join(root, "versions", "0c73a25e.zip"))
	t.Setenv("PATH", t.TempDir())

	got := Detect(updatingSpec(home, root), home)
	if !got.Installed || !got.Updating {
		t.Fatalf("应判定更新中, got %+v", got)
	}
	if got.Source != "updating" {
		t.Fatalf("Source = %q, want updating", got.Source)
	}
	if got.Broken {
		t.Fatal("更新中不得标记 Broken（避免触发自动修复与更新器竞争）")
	}
}

// 更新器刚清掉 shim、zip 尚未落地的起步形态：根目录 mtime 新鲜 → 更新中。
func TestDetectUpdatingByRecentMtime(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cursor-agent")
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 目录拨旧后碰一下根目录（建再删一个文件），模拟更新器刚动过
	agePath(t, root, 5*time.Minute)
	agePath(t, filepath.Join(root, "versions"), 5*time.Minute)
	touch := filepath.Join(root, "transient")
	if err := os.WriteFile(touch, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(touch); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	got := Detect(updatingSpec(home, root), home)
	if !got.Installed || !got.Updating || got.Broken {
		t.Fatalf("根目录刚被写入应判定更新中, got %+v", got)
	}
}

// zip 是历史残留（20 分钟前）、目录 mtime 也陈旧 → 落回 config-dir 残缺，不算更新中。
func TestDetectStaleZipNotUpdating(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cursor-agent")
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, filepath.Join(root, "versions", "stale.zip"))
	agePath(t, filepath.Join(root, "versions", "stale.zip"), 20*time.Minute)
	agePath(t, root, 20*time.Minute)
	agePath(t, filepath.Join(root, "versions"), 20*time.Minute)
	t.Setenv("PATH", t.TempDir())

	got := Detect(updatingSpec(home, root), home)
	if got.Updating {
		t.Fatalf("陈旧 zip 不得判定更新中, got %+v", got)
	}
	if got.Source != "config-dir" || !got.Broken {
		t.Fatalf("应落回 config-dir 残缺, got %+v", got)
	}
}

// 健康安装（shim 存在）即使目录新鲜也不得误报更新中。
func TestDetectHealthyNotUpdating(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cursor-agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeBin(t, root, "cursor-agent", "echo ok")
	t.Setenv("PATH", t.TempDir())

	got := Detect(updatingSpec(home, root), home)
	if !got.Installed || got.Updating {
		t.Fatalf("健康安装不得判定更新中, got %+v", got)
	}
}

// 平台契约：更新中判定仅 Windows 生效。非 Windows 的 InstallDirs 是共享 bin 目录
// 且无 zip 自更新机制，误报没有任何真阳性补偿。
func TestDetectUpdatingWindowsOnly(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cursor-agent")
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, filepath.Join(root, "versions", "fresh.zip"))
	t.Setenv("PATH", t.TempDir())

	got := Detect(updatingSpec(home, root), home)
	if runtime.GOOS == "windows" {
		if !got.Updating {
			t.Fatalf("Windows 上应判定更新中, got %+v", got)
		}
		return
	}
	if got.Updating {
		t.Fatalf("非 Windows 不得判定更新中, got %+v", got)
	}
	if got.Source != "config-dir" || !got.Broken {
		t.Fatalf("非 Windows 应落回 config-dir 残缺, got %+v", got)
	}
}

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

// 仅 config-dir 命中：配置在、可执行入口全无 → 残缺（Broken）。
func TestDetectConfigDirOnlyMarksBroken(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	spec := DetectSpec{
		BinName:     "cursor-agent",
		InstallDirs: []string{filepath.Join(home, "no-such-dir")},
		ConfigDirs:  []string{"~/.cursor"},
	}
	got := Detect(spec, home)
	if !got.Installed || got.Source != "config-dir" {
		t.Fatalf("got %+v, want installed via config-dir", got)
	}
	if !got.Broken {
		t.Fatal("仅 config-dir 命中应 Broken=true（配置在、入口全无）")
	}
}

// 有可执行入口（install-dir 命中）→ 健康，Broken 必须为 false。
func TestDetectHealthyNotBroken(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeBin(t, dir, "cursor-agent", "echo ok")
	t.Setenv("PATH", t.TempDir())
	spec := DetectSpec{
		BinName:     "cursor-agent",
		InstallDirs: []string{"~/.local/bin"},
		ConfigDirs:  []string{"~/.cursor"},
	}
	got := Detect(spec, home)
	if got.Source != "install-dir" || got.Broken {
		t.Fatalf("got %+v, want install-dir/healthy", got)
	}
}

// 全不中：未安装，也不算残缺。
func TestDetectAbsentNotBroken(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := Detect(DetectSpec{BinName: "no-such-tool"}, t.TempDir())
	if got.Installed || got.Broken {
		t.Fatalf("got %+v, want absent/healthy", got)
	}
}

// config-dir 判定的 marker 门槛：声明 ConfigMarkers 后，配置目录必须含任一
// 有效产物才算「用过」。kshell 技能广场会往 ~/.gemini/skills 写技能，
// 不能自造目录就把未安装的工具判成「已损坏」。
func TestDetectConfigDirMarkerGates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	// 只有 skills/（kshell 自写产物）：不算安装。
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := DetectSpec{
		BinName:       "gemini",
		ConfigDirs:    []string{"~/.gemini"},
		ConfigMarkers: []string{"settings.json", "oauth_creds.json"},
	}
	if got := Detect(spec, home); got.Installed {
		t.Fatalf("只有 skills/ 不得算安装, got %+v", got)
	}
	// 文件 marker 出现 → config-dir 命中（Broken：无可执行入口）。
	writeRawFile(t, filepath.Join(home, ".gemini", "settings.json"))
	if got := Detect(spec, home); !got.Installed || got.Source != "config-dir" || !got.Broken {
		t.Fatalf("marker 命中应 config-dir/Broken, got %+v", got)
	}
	// 目录 marker 同理：~/.trae 被 IDE 共享时不算，cli/ 子树存在才算。
	spec2 := DetectSpec{BinName: "trae-cli", ConfigDirs: []string{"~/.trae"}, ConfigMarkers: []string{"cli"}}
	if err := os.MkdirAll(filepath.Join(home, ".trae"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Detect(spec2, home); got.Installed {
		t.Fatalf("~/.trae 无 cli 子树不得算安装, got %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".trae", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Detect(spec2, home); !got.Installed || got.Source != "config-dir" {
		t.Fatalf("cli 子树存在应 config-dir 命中, got %+v", got)
	}
}

// 未声明 ConfigMarkers 的 provider 保持旧行为：配置目录存在即命中。
func TestDetectConfigDirNoMarkersKeepsLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".claude", "whatever"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Detect(DetectSpec{BinName: "claude", ConfigDirs: []string{"~/.claude"}}, home)
	if !got.Installed || got.Source != "config-dir" {
		t.Fatalf("无 marker 应保持旧行为, got %+v", got)
	}
}

// Gemini 实际规格：技能广场写入的 skills/ 不构成安装信号（用户截图实证的误判）。
func TestGeminiDetectSpecSkillsOnlyNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Detect((Gemini{}).DetectSpec(home), home); got.Installed {
		t.Fatalf("~/.gemini 只有 skills/ 应判未安装, got %+v", got)
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
