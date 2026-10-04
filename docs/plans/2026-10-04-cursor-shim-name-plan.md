# Cursor shim 改名修复 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development 逐任务实现（本任务强耦合，在当前会话连续执行）。

**Goal:** 让 kshell 识别 Cursor 官方安装器新版 shim 名 `agent.{cmd,ps1}`，重启后 Cursor 正常显示已安装。

**Architecture:** `DetectSpec` 新增 `AltBinNames`；`Detect`/`FindBins` 在 InstallDirs 内对全部名字找候选，PATH 探测仍只用 `BinName`。Cursor 声明 `AltBinNames: ["agent"]`。

**Tech Stack:** Go 1.23+，标准库 `os/exec`。

---

### Task 1: DetectSpec.AltBinNames 与 InstallDirs 探测

**Files:**
- Modify: `internal/providers/provider.go:21-25`（DetectSpec 结构体）
- Modify: `internal/providers/detect.go`（Detect / FindBins）
- Modify: `internal/providers/cursor.go:27-41`（Cursor.DetectSpec）
- Test: `internal/providers/cursor_test.go`

**Step 1: 写失败测试**

在 `cursor_test.go` 的 `TestCursorDetectFindsVersionedInstallDir` 之后追加：

```go
// 官方安装器 2026-10 起把根目录 shim 改名为 agent.{cmd,ps1}，不再有 cursor-agent.*。
func TestCursorDetectFindsAgentShim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	var dir string
	if runtime.GOOS == "windows" {
		local := t.TempDir()
		t.Setenv("LOCALAPPDATA", local)
		dir = filepath.Join(local, "cursor-agent")
	} else {
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeBin(t, dir, "agent", "echo agent")

	got := Detect(Cursor{}.DetectSpec(home), home)
	if !got.Installed || got.Source != "install-dir" {
		t.Fatalf("got %+v, want install-dir via agent shim", got)
	}
	if filepath.Clean(got.BinPath) != filepath.Clean(bin) {
		t.Fatalf("BinPath = %q, want %q", got.BinPath, bin)
	}
}

// agent 名字太通用，只能出现在已知安装目录的探测里，不能进 PATH 探测。
func TestAgentShimNotProbedOnPath(t *testing.T) {
	home := t.TempDir()
	pathDir := t.TempDir()
	t.Setenv("PATH", pathDir)
	writeFakeBin(t, pathDir, "agent", "echo agent")

	got := Detect(Cursor{}.DetectSpec(home), home)
	if got.Installed {
		t.Fatalf("PATH 上的 agent 不应让 cursor 判定为已安装: %+v", got)
	}
}
```

**Step 2: 跑测试确认失败**

```
go test ./internal/providers -run TestCursorDetectFindsAgentShim -count=1
```
预期：FAIL（Detect 返回 `Source:"config-dir"` 或 `Installed:false`）。

**Step 3: 最小实现**

`provider.go` DetectSpec 增加：

```go
type DetectSpec struct {
	BinName     string   // PATH 上查找的可执行名
	AltBinNames []string // 备选可执行名：只在 InstallDirs 内匹配，不进 PATH 探测（防误命中同名无关文件）
	InstallDirs []string // 常见安装目录（支持 ~ 前缀与环境变量）
	ConfigDirs  []string // 存在即说明装过（支持 ~ 前缀）
}
```

`detect.go`：新增 `specBinNames` 辅助，Detect 的 InstallDirs 循环与 FindBins 改用它：

```go
// specBinNames 返回 InstallDirs 内要尝试的全部可执行名：BinName 永远在前优先命中。
func specBinNames(spec DetectSpec) []string {
	names := []string{spec.BinName}
	return append(names, spec.AltBinNames...)
}
```

`Detect` 的 PATH 段不变（只 LookPath `spec.BinName`）；InstallDirs 段改为：

```go
	for _, dir := range spec.InstallDirs {
		root := expandHome(dir, home)
		for _, name := range specBinNames(spec) {
			for _, candidate := range binCandidates(root, name) {
				if isFile(candidate) {
					return Detection{Installed: true, BinPath: candidate, Source: "install-dir"}
				}
			}
		}
	}
```

`FindBins` 的 InstallDirs 段同样按 `specBinNames(spec)` 展开。

`cursor.go` DetectSpec 返回值加 `AltBinNames: []string{"agent"}`。

**Step 4: 跑测试确认通过**

```
go test ./internal/providers -run "TestCursorDetect|TestAgentShim" -count=1
```
预期：PASS。

**Step 5: 提交**

```
git add -A && git commit -m "fix: 探测 Cursor 官方安装器改名后的 agent shim"
```

### Task 2: FindBins 卸载覆盖与回归验证

**Files:**
- Test: `internal/providers/install_test.go`（若已有 FindBins 相关测试则就地补，否则追加）

**Step 1: 写失败测试**

```go
// 卸载要删掉每一份拷贝：根目录同时有旧名与新名 shim 时，FindBins 都要列出来。
func TestFindBinsCoversAltNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 才有多后缀候选")
	}
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	dir := filepath.Join(local, "cursor-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := writeFakeBin(t, dir, "cursor-agent", "echo cursor-agent")
	new := writeFakeBin(t, dir, "agent", "echo agent")

	got := FindBins(Cursor{}.DetectSpec(home), home)
	if len(got) != 2 {
		t.Fatalf("FindBins = %v, want 旧名与新名两份", got)
	}
	for _, p := range got {
		if filepath.Clean(p) != filepath.Clean(old) && filepath.Clean(p) != filepath.Clean(new) {
			t.Fatalf("意外路径 %q", p)
		}
	}
}
```

**Step 2: 跑测试**——Task 1 已把 FindBins 改好，此测试应当直接 PASS（它守护的是 Task 1 实现的完整性；若 FAIL 则回到 Task 1 修 FindBins）。

**Step 3: 全量验证**

```
go build ./... ; go vet ./... ; go test ./... -count=1
```
预期：全绿。

**Step 4: 提交**

```
git add -A && git commit -m "test: 覆盖 FindBins 卸载覆盖新旧 shim 名"
```

### Task 3: 冒烟条目与收尾

**Step 1:** 在 `docs/smoke/fix-cursor-shim-name.md` 追加增量条目：重启桌面端后设置页 Cursor 显示已安装、新建会话下拉含 Cursor。

**Step 2:** 提交 `docs: 冒烟条目`。
