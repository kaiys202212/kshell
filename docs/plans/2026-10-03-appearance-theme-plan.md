# kshell 颜色模式（跟随系统明暗）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 kshell 增加 `system/light/dark` 颜色模式配置，驱动桌面 UI、TUI、内嵌终端、原生窗口与所启动 agent 工具的主题，跟随操作系统明暗。

**Architecture:** 新增 `internal/appearance` 作为唯一主题源（模式解析、OS 注册表探测、变化监听、通用颜色环境变量）。配置增加 `appearance.mode`；桌面端提供 `GetAppearance/SetAppearanceMode` 绑定与 `appearance:changed` 事件；各 provider 通过可选 `Themer` 接口声明工具专属注入；`internal/launch` 统一套用；TUI/前端/xterm 只做呈现。

**Tech Stack:** Go 1.2x + `golang.org/x/sys/windows/registry`、`gopkg.in/yaml.v3`、lipgloss/termenv；Wails v2、React 19 + Tailwind v4 + Zustand + xterm、vitest。

> 说明：按 AGENTS.md，提交只在用户明确要求时执行；下面各任务的 Commit 步骤在用户同意后再运行。

---

## 文件结构

**新增**
- `internal/appearance/appearance.go` — Mode/Theme 类型、ParseMode、Resolve、GenericEnv。
- `internal/appearance/detect_windows.go` / `detect_other.go` — OS 明暗探测（构建约束）。
- `internal/appearance/watcher.go` — system 模式下的变化轮询。
- `internal/config/appearance_test.go`、`internal/launch/theme_test.go`、`internal/providers/theme_inject.go` + `theme_inject_test.go`、`internal/terminal/env_test.go`、`internal/desktop/appearance.go` + `appearance_windows.go` + `appearance_other.go` + `appearance_test.go`、`frontend/src/lib/appearance.ts` + `appearance.test.ts`。

**修改**
- `internal/config/config.go`、`internal/config/paths.go`
- `internal/providers/provider.go`、`claude.go`、`codex.go`、`gemini.go`、`opencode.go`
- `internal/launch/launch.go`、`internal/launcher/launcher.go`、`internal/terminal/backend_pty.go`
- `internal/ui/theme.go`、`internal/ui/app.go`、`cmd/kshell/main.go`、`internal/ui/app_test.go`、`internal/ui/help_test.go`
- `internal/desktop/app.go`、`internal/desktop/terminal.go`、`internal/desktop/app_test.go`
- `frontend/src/style.css`、`frontend/index.html`、`frontend/src/lib/api.ts`、`frontend/src/state/store.ts`、`frontend/src/App.tsx`、`frontend/src/pages/Settings.tsx`、`frontend/src/components/TerminalView.tsx`、`frontend/src/pages/Settings.test.tsx`、`frontend/src/components/TerminalView.test.tsx`

---

## Task 1: appearance 核心（类型/解析/OS 探测/通用环境变量）

**Files:**
- Create: `internal/appearance/appearance.go`, `internal/appearance/detect_windows.go`, `internal/appearance/detect_other.go`
- Test: `internal/appearance/appearance_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/appearance/appearance_test.go`：

```go
package appearance

import "testing"

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":        System,
		"system":  System,
		"SYSTEM":  System,
		"bogus":   System,
		"light":   Light,
		" Light ": Light,
		"dark":    Dark,
		"DARK":    Dark,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveForcedModesIgnoreOS(t *testing.T) {
	prev := detectOSTheme
	detectOSTheme = func() Theme { return ThemeDark }
	t.Cleanup(func() { detectOSTheme = prev })

	if got := Resolve(Light); got != ThemeLight {
		t.Fatalf("Resolve(light) = %q", got)
	}
	if got := Resolve(Dark); got != ThemeDark {
		t.Fatalf("Resolve(dark) = %q", got)
	}
	if got := Resolve(System); got != ThemeDark {
		t.Fatalf("Resolve(system) = %q, want detected dark", got)
	}
}

func TestGenericEnv(t *testing.T) {
	dark := GenericEnv(ThemeDark)
	if dark["COLORFGBG"] != "15;0" {
		t.Fatalf("dark COLORFGBG = %q", dark["COLORFGBG"])
	}
	if dark["COLORTERM"] != "truecolor" {
		t.Fatalf("COLORTERM = %q", dark["COLORTERM"])
	}
	light := GenericEnv(ThemeLight)
	if light["COLORFGBG"] != "0;15" {
		t.Fatalf("light COLORFGBG = %q", light["COLORFGBG"])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/appearance/ -run 'TestParseMode|TestResolve|TestGenericEnv' -count=1`
Expected: FAIL（包/符号未定义）

- [ ] **Step 3: 实现**

创建 `internal/appearance/appearance.go`：

```go
// Package appearance 是 kshell 的颜色模式唯一来源：解析用户配置的模式、探测操作系统明暗、
// 监听系统变化，并向各界面/启动器提供通用颜色环境变量。各界面只消费 Theme，不自行判断。
package appearance

import "strings"

// Mode 是用户可配置的颜色模式。
type Mode string

const (
	System Mode = "system"
	Light  Mode = "light"
	Dark   Mode = "dark"
)

// Theme 是解析后的明暗结果。
type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// ParseMode 归一化模式字符串；非法或空值一律回落 system。
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(Light):
		return Light
	case string(Dark):
		return Dark
	default:
		return System
	}
}

// detectOSTheme 是可替换的探测入口：windows 读注册表，其它平台兜底深色。
// 抽成变量便于单测注入。
var detectOSTheme = detectOSThemePlatform

// Resolve 把模式解析为具体明暗：light/dark 直接返回，system 走 OS 探测。
func Resolve(mode Mode) Theme {
	switch mode {
	case Light:
		return ThemeLight
	case Dark:
		return ThemeDark
	default:
		return detectOSTheme()
	}
}

// GenericEnv 返回对所有工具通用的颜色环境变量：
// COLORFGBG（前景;背景，背景 0-6/8 为暗、7/9-15 为亮）与 COLORTERM=truecolor。
func GenericEnv(t Theme) map[string]string {
	fg, bg := "15", "0"
	if t == ThemeLight {
		fg, bg = "0", "15"
	}
	return map[string]string{
		"COLORFGBG": fg + ";" + bg,
		"COLORTERM": "truecolor",
	}
}
```

创建 `internal/appearance/detect_windows.go`：

```go
//go:build windows

package appearance

import "golang.org/x/sys/windows/registry"

// detectOSThemePlatform 读注册表 AppsUseLightTheme：1=浅色、0=深色；读不到按深色。
func detectOSThemePlatform() Theme {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ThemeDark
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil || v == 0 {
		return ThemeDark
	}
	return ThemeLight
}
```

创建 `internal/appearance/detect_other.go`：

```go
//go:build !windows

package appearance

// detectOSThemePlatform 非 Windows 平台暂无系统级 API，兜底深色（TUI 另有终端背景探测）。
func detectOSThemePlatform() Theme { return ThemeDark }
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/appearance/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/appearance/appearance.go internal/appearance/detect_windows.go internal/appearance/detect_other.go internal/appearance/appearance_test.go
git commit -m "feat(appearance): 颜色模式核心类型、解析与 OS 探测"
```

---

## Task 2: OS 明暗变化监听

**Files:**
- Create: `internal/appearance/watcher.go`
- Test: `internal/appearance/watcher_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/appearance/watcher_test.go`：

```go
package appearance

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatcherEmitsOnlyOnChange(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	w := NewWatcher(func(Theme) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	w.interval = time.Millisecond

	seq := []Theme{ThemeDark, ThemeDark, ThemeLight, ThemeLight, ThemeLight}
	var i int
	w.probe = func() Theme {
		mu.Lock()
		defer mu.Unlock()
		v := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return v
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("onChange called %d times, want 1", n)
	}
}

func TestWatcherStopsOnCancel(t *testing.T) {
	w := NewWatcher(nil)
	w.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/appearance/ -run TestWatcher -count=1`
Expected: FAIL（`NewWatcher`/`Run` 未定义）

- [ ] **Step 3: 实现监听**

创建 `internal/appearance/watcher.go`：

```go
package appearance

import (
	"context"
	"time"
)

// Watcher 周期性探测系统明暗，仅在变化时回调。字段非导出，测试可直接改。
type Watcher struct {
	interval time.Duration
	probe    func() Theme
	onChange func(Theme)
}

// NewWatcher 创建监听器，onChange 可为 nil。
func NewWatcher(onChange func(Theme)) *Watcher {
	return &Watcher{
		interval: 3 * time.Second,
		probe:    func() Theme { return detectOSTheme() },
		onChange: onChange,
	}
}

// Run 阻塞轮询直到 ctx 取消。首次探测作为基线，不回调。
func (w *Watcher) Run(ctx context.Context) {
	last := w.probe()
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := w.probe(); cur != last {
				last = cur
				if w.onChange != nil {
					w.onChange(cur)
				}
			}
		}
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/appearance/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/appearance/
git commit -m "feat(appearance): 系统明暗变化监听"
```

---

## Task 3: 配置增加 appearance.mode

**Files:**
- Modify: `internal/config/config.go`, `internal/config/paths.go`
- Test: `internal/config/appearance_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/config/appearance_test.go`：

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestAppearanceDefaultIsSystem(t *testing.T) {
	if got := Default().Appearance.Mode; got != "system" {
		t.Fatalf("default mode = %q, want system", got)
	}
}

func TestAppearanceInvalidFallsBackToSystem(t *testing.T) {
	c := Config{MaxDepth: 1, Appearance: Appearance{Mode: "bogus"}}
	if got := c.normalized().Appearance.Mode; got != "system" {
		t.Fatalf("normalized mode = %q, want system", got)
	}
}

func TestAppearanceSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := Default()
	cfg.Appearance.Mode = "dark"
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Appearance.Mode != "dark" {
		t.Fatalf("loaded mode = %q, want dark", loaded.Appearance.Mode)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run TestAppearance -count=1`
Expected: FAIL（`Appearance` 未定义）

- [ ] **Step 3: 修改配置**

在 `internal/config/config.go` 顶部（`SSHOptions` 之后）加入：

```go
// Appearance 是颜色模式配置：system 跟随操作系统，light/dark 强制覆盖。
type Appearance struct {
	Mode string `yaml:"mode"` // system | light | dark
}
```

在 `Config` 结构体加字段：

```go
type Config struct {
	ScanRoots  []string        `yaml:"scan_roots"`
	MaxDepth   int             `yaml:"max_depth"`
	Exclude    []string        `yaml:"exclude"`
	SSHOptions SSHOptions      `yaml:"ssh"`
	Scanners   map[string]bool `yaml:"scanners"`
	Appearance Appearance      `yaml:"appearance"`
}
```

在 `Default()` 返回值里加：

```go
		Appearance: Appearance{Mode: "system"},
```

在 `normalized()` 的 `return c` 之前加：

```go
	// 颜色模式：空值或非法值一律回落默认（system）。
	switch c.Appearance.Mode {
	case "system", "light", "dark":
	default:
		c.Appearance.Mode = d.Appearance.Mode
	}
```

- [ ] **Step 4: 增加缓存目录路径**

在 `internal/config/paths.go` 的 `Layout` 结构体加字段：

```go
	// CacheAppearance 是颜色主题注入文件的落盘目录（agent 工具用）。
	CacheAppearance string
```

在 `Paths()` 返回的 `Layout{...}` 里加：

```go
		CacheAppearance: filepath.Join(cache, "appearance"),
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/config/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "feat(config): 新增 appearance.mode 与主题缓存目录"
```

---

## Task 4: providers 主题注入能力

**Files:**
- Modify: `internal/providers/provider.go`, `claude.go`, `codex.go`, `gemini.go`, `opencode.go`
- Create: `internal/providers/theme_inject.go`, `internal/providers/theme_inject_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/providers/theme_inject_test.go`：

```go
package providers

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
)

func TestClaudeThemeOverrides(t *testing.T) {
	args, env := Claude{}.ThemeOverrides(appearance.ThemeDark, "")
	if env != nil {
		t.Fatalf("env = %v, want nil", env)
	}
	if len(args) != 2 || args[0] != "--settings" || args[1] != `{"theme":"dark"}` {
		t.Fatalf("args = %v", args)
	}
	args, _ = Claude{}.ThemeOverrides(appearance.ThemeLight, "")
	if args[1] != `{"theme":"light"}` {
		t.Fatalf("args = %v", args)
	}
}

func TestCodexThemeOverrides(t *testing.T) {
	dark, _ := Codex{}.ThemeOverrides(appearance.ThemeDark, "")
	if len(dark) != 2 || dark[0] != "-c" || dark[1] != "tui.theme=catppuccin-mocha" {
		t.Fatalf("dark args = %v", dark)
	}
	light, _ := Codex{}.ThemeOverrides(appearance.ThemeLight, "")
	if light[1] != "tui.theme=catppuccin-latte" {
		t.Fatalf("light args = %v", light)
	}
}

func TestGeminiWritesSystemSettings(t *testing.T) {
	dir := t.TempDir()
	_, env := Gemini{}.ThemeOverrides(appearance.ThemeLight, dir)
	path := env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"]
	if path == "" {
		t.Fatal("missing GEMINI_CLI_SYSTEM_SETTINGS_PATH")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		UI struct {
			Theme string `json:"theme"`
			Auto  bool   `json:"autoThemeSwitching"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.UI.Theme != "Default Light" || got.UI.Auto {
		t.Fatalf("ui = %+v", got.UI)
	}
}

func TestOpencodeWritesTUIConfig(t *testing.T) {
	dir := t.TempDir()
	_, env := Opencode{}.ThemeOverrides(appearance.ThemeDark, dir)
	path := env["OPENCODE_TUI_CONFIG"]
	if path == "" {
		t.Fatal("missing OPENCODE_TUI_CONFIG")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"theme":"system"}` {
		t.Fatalf("data = %s", data)
	}
}

func TestFileInjectSkippedWithoutCacheDir(t *testing.T) {
	if _, env := Gemini{}.ThemeOverrides(appearance.ThemeDark, ""); env != nil {
		t.Fatalf("gemini env = %v", env)
	}
	if _, env := Opencode{}.ThemeOverrides(appearance.ThemeDark, ""); env != nil {
		t.Fatalf("opencode env = %v", env)
	}
}

func TestEnvListSortedAndNil(t *testing.T) {
	got := EnvList(map[string]string{"B": "2", "A": "1"})
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("EnvList = %v", got)
	}
	if EnvList(nil) != nil {
		t.Fatal("EnvList(nil) should be nil")
	}
}

func TestMergeEnvOverrides(t *testing.T) {
	got := MergeEnv(map[string]string{"A": "1", "B": "1"}, map[string]string{"B": "2", "C": "3"})
	if got["A"] != "1" || got["B"] != "2" || got["C"] != "3" {
		t.Fatalf("MergeEnv = %v", got)
	}
	if MergeEnv(nil, nil) != nil {
		t.Fatal("MergeEnv(nil,nil) should be nil")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/providers/ -run 'TestClaudeTheme|TestCodexTheme|TestGemini|TestOpencode|TestFileInject|TestEnvList|TestMergeEnv' -count=1`
Expected: FAIL（`ThemeOverrides` 等未定义）

- [ ] **Step 3: 扩展 Launch 与可选接口**

在 `internal/providers/provider.go`：

把 `Launch` 改为：

```go
// Launch 描述一次进程启动：会话恢复与新建会话都用它，交给 launcher 统一处理（含 Windows 的 .ps1 shim）。
type Launch struct {
	Path string
	Args []string
	Dir  string
	// Env 是额外环境变量；nil 表示仅继承当前进程环境，非空时会叠加在父环境之上。
	Env map[string]string
}
```

在 `provider.go` 的 import 增加 `"github.com/yangk/kshell/internal/appearance"`，并在 `SessionEnumerator` 接口后加：

```go
// Themer 可选接口：provider 声明如何让自身 TUI 跟随浅/深主题。
// args 追加到启动参数；env 合并进子进程环境；cacheDir 供需要落临时配置的工具使用
// （写入 kshell 自己的缓存目录，绝不修改工具自身的用户配置）。
type Themer interface {
	ThemeOverrides(theme appearance.Theme, cacheDir string) (args []string, env map[string]string)
}
```

- [ ] **Step 4: 新增注入辅助**

创建 `internal/providers/theme_inject.go`：

```go
package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// MergeEnv 合并两组环境变量，b 覆盖 a；两者皆空返回 nil。不修改入参。
func MergeEnv(a, b map[string]string) map[string]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// EnvList 把环境变量展开成 "K=V" 列表（按 key 排序保证确定性）；空 map 返回 nil。
func EnvList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// writeThemeJSON 把主题覆盖配置写到 kshell 缓存目录（0600），返回文件路径。
func writeThemeJSON(dir, name string, content any) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
```

- [ ] **Step 5: 各 provider 实现 ThemeOverrides**

在 `internal/providers/claude.go`：import 增加 appearance，文件末尾加：

```go
// ThemeOverrides 用会话级 --settings 注入主题，不写用户的 settings.json。
func (Claude) ThemeOverrides(theme appearance.Theme, _ string) ([]string, map[string]string) {
	name := "light"
	if theme == appearance.ThemeDark {
		name = "dark"
	}
	return []string{"--settings", `{"theme":"` + name + `"}`}, nil
}
```

在 `internal/providers/codex.go`：import 增加 appearance，末尾加：

```go
// ThemeOverrides 用 -c 覆盖 tui.theme（会话级，不写 config.toml）。
func (Codex) ThemeOverrides(theme appearance.Theme, _ string) ([]string, map[string]string) {
	name := "catppuccin-latte"
	if theme == appearance.ThemeDark {
		name = "catppuccin-mocha"
	}
	return []string{"-c", "tui.theme=" + name}, nil
}
```

在 `internal/providers/gemini.go`：import 增加 appearance，末尾加：

```go
// ThemeOverrides 指向生成的系统级 settings（最高优先级层），并关闭自动主题轮询。
func (Gemini) ThemeOverrides(theme appearance.Theme, cacheDir string) ([]string, map[string]string) {
	if cacheDir == "" {
		return nil, nil
	}
	name := "Default Light"
	if theme == appearance.ThemeDark {
		name = "Default"
	}
	path, err := writeThemeJSON(cacheDir, "gemini-"+string(theme)+".json", map[string]any{
		"ui": map[string]any{"theme": name, "autoThemeSwitching": false},
	})
	if err != nil {
		return nil, nil
	}
	return nil, map[string]string{"GEMINI_CLI_SYSTEM_SETTINGS_PATH": path}
}
```

在 `internal/providers/opencode.go`：import 增加 appearance，末尾加：

```go
// ThemeOverrides 指向生成的 tui.json，使用 system 主题（按承载终端背景自适应）。
func (Opencode) ThemeOverrides(theme appearance.Theme, cacheDir string) ([]string, map[string]string) {
	if cacheDir == "" {
		return nil, nil
	}
	path, err := writeThemeJSON(cacheDir, "opencode-"+string(theme)+".json", map[string]any{"theme": "system"})
	if err != nil {
		return nil, nil
	}
	return nil, map[string]string{"OPENCODE_TUI_CONFIG": path}
}
```

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/providers/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/providers/
git commit -m "feat(providers): 各工具主题注入能力（Themer）"
```

---

## Task 5: launch 套用主题 + 环境变量透传

**Files:**
- Modify: `internal/launch/launch.go`, `internal/launcher/launcher.go`, `internal/terminal/backend_pty.go`
- Create: `internal/launch/theme_test.go`, `internal/terminal/env_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/launch/theme_test.go`：

```go
package launch

import (
	"reflect"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func TestForSessionInjectsGenericAndProviderTheme(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude.exe"}}
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: `D:\ws`}

	l, err := ForSession(ps, tools, s, ThemeOptions{Mode: appearance.Light})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["COLORFGBG"] != "0;15" || l.Env["COLORTERM"] != "truecolor" {
		t.Fatalf("env = %v", l.Env)
	}
	want := []string{"--resume", "s1", "--settings", `{"theme":"light"}`}
	if !reflect.DeepEqual(l.Args, want) {
		t.Fatalf("args = %v, want %v", l.Args, want)
	}
}

func TestForWorkspaceToolGenericOnlyForNonThemer(t *testing.T) {
	ps := []providers.Provider{providers.Generic{Spec: providers.GenericSpec{ID: "gtool"}}}
	tools := []discovery.Tool{{ID: "gtool", Installed: true, BinPath: "gtool.exe"}}
	ws := discovery.Workspace{Path: `D:\ws`}

	l, err := ForWorkspaceTool(ps, tools, ws, "gtool", ThemeOptions{Mode: appearance.Dark})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["COLORFGBG"] != "15;0" {
		t.Fatalf("env = %v", l.Env)
	}
	if len(l.Args) != 0 {
		t.Fatalf("args = %v, want empty", l.Args)
	}
}

func TestForSessionGeminiWritesFileEnv(t *testing.T) {
	dir := t.TempDir()
	ps := []providers.Provider{providers.Gemini{}}
	tools := []discovery.Tool{{ID: "gemini", Installed: true, BinPath: "gemini.exe"}}
	s := providers.Session{ID: "g1", ToolID: "gemini", Workspace: `D:\ws`}

	l, err := ForSession(ps, tools, s, ThemeOptions{Mode: appearance.Dark, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] == "" {
		t.Fatalf("env = %v", l.Env)
	}
}
```

创建 `internal/terminal/env_test.go`：

```go
package terminal

import (
	"strings"
	"testing"
)

func TestMergedEnvNilWhenEmpty(t *testing.T) {
	if got := mergedEnv(nil); got != nil {
		t.Fatalf("mergedEnv(nil) = %v, want nil", got)
	}
}

func TestMergedEnvIncludesParent(t *testing.T) {
	t.Setenv("KSHELL_TEST_PARENT", "1")
	got := mergedEnv([]string{"COLORFGBG=15;0"})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "COLORFGBG=15;0") {
		t.Fatalf("missing extra env: %v", got)
	}
	if !strings.Contains(joined, "KSHELL_TEST_PARENT=1") {
		t.Fatalf("missing parent env: %v", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/launch/ ./internal/terminal/ -run 'TestForSession|TestForWorkspaceTool|TestMergedEnv' -count=1`
Expected: FAIL（签名/函数未定义）

- [ ] **Step 3: 改 launch 套用主题**

在 `internal/launch/launch.go`：import 增加 `"github.com/yangk/kshell/internal/appearance"`；在 `ErrToolNotRunnable` 后加：

```go
// ThemeOptions 描述主题注入所需信息：模式 + 生成文件落盘目录（空则跳过文件类注入）。
type ThemeOptions struct {
	Mode     appearance.Mode
	CacheDir string
}

// applyTheme 给启动描述注入主题：总是追加通用颜色变量，provider 实现 Themer 时再追加专属参数/变量。
func applyTheme(l providers.Launch, p providers.Provider, to ThemeOptions) providers.Launch {
	theme := appearance.Resolve(to.Mode)
	l.Env = providers.MergeEnv(l.Env, appearance.GenericEnv(theme))
	if th, ok := p.(providers.Themer); ok {
		args, env := th.ThemeOverrides(theme, to.CacheDir)
		l.Args = append(l.Args, args...)
		l.Env = providers.MergeEnv(l.Env, env)
	}
	return l
}
```

将 `ForSession`、`ForWorkspace`、`ForWorkspaceTool` 改为接收 `to ThemeOptions` 并套用：

```go
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session, to ThemeOptions) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return applyTheme(p.ResumeCmd(s, tool.BinPath), p, to), nil
}

func ForWorkspace(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, to ThemeOptions) (providers.Launch, error) {
	return ForWorkspaceTool(ps, tools, ws, "", to)
}

func ForWorkspaceTool(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, to ThemeOptions) (providers.Launch, error) {
	if toolID == "" {
		p, tool, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		return applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), nil
	}

	tool, ok := toolFor(tools, toolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, toolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), nil
}
```

- [ ] **Step 4: launcher.Spec 携带 Env**

在 `internal/launcher/launcher.go`：`Spec` 加字段：

```go
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string // 额外环境变量（"K=V"）；空表示仅继承
}
```

`Build` 里在 `spec.Dir = l.Dir` 后加：

```go
	spec.Env = providers.EnvList(l.Env)
```

`Spec.Cmd` 里在设置 `cmd.Dir` 后加：

```go
	if len(s.Env) > 0 {
		cmd.Env = append(os.Environ(), s.Env...)
	}
```

import 增加 `"os"`。

- [ ] **Step 5: 内嵌终端叠加父环境**

在 `internal/terminal/backend_pty.go` 把 `cmd.Env = spec.Env` 改为：

```go
	cmd.Env = mergedEnv(spec.Env)
```

并在文件末尾加：

```go
// mergedEnv 把额外环境变量叠加到父进程环境之上；空表示完全继承（返回 nil）。
func mergedEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}
```

（`os` 已在 import 中。）

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/launch/ ./internal/terminal/ ./internal/launcher/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/launch/ internal/launcher/ internal/terminal/
git commit -m "feat(launch): 统一注入主题环境变量并透传到子进程"
```

---

## Task 6: TUI 浅色/深色主题

**Files:**
- Modify: `internal/ui/theme.go`, `internal/ui/app.go`, `cmd/kshell/main.go`
- Modify: `internal/ui/app_test.go`, `internal/ui/help_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/ui/app_test.go` 把两处 `NewTheme()` 改为 `NewTheme(appearance.Dark)`（import 增加 `"github.com/yangk/kshell/internal/appearance"`），并新增：

```go
func TestNewThemeLightAndDarkDiffer(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	t.Setenv("NO_COLOR", "")

	dark := NewTheme(appearance.Dark).Body.Render("x")
	light := NewTheme(appearance.Light).Body.Render("x")
	if dark == light {
		t.Fatalf("light 与 dark 正文渲染不应相同: %q", dark)
	}
}
```

在 `internal/ui/help_test.go` 把 `NewTheme()` 改为 `NewTheme(appearance.Dark)`（并 import appearance）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/ui/ -run 'TestTheme|TestNewTheme' -count=1`
Expected: FAIL（`NewTheme` 参数不匹配）

- [ ] **Step 3: 实现双调色板**

将 `internal/ui/theme.go` 整体替换为：

```go
package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yangk/kshell/internal/appearance"
)

// Theme 集中定义配色，避免样式散落在各面板里。
type Theme struct {
	Title     lipgloss.Style
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Header    lipgloss.Style
	Muted     lipgloss.Style
	Body      lipgloss.Style
	Preview   lipgloss.Style
	StatusBar lipgloss.Style
}

// NewTheme 按配置模式构建主题；system 模式跟随终端实际背景（比注册表更贴近 TUI 呈现环境）。
// NO_COLOR 优先级最高，直接降级为无色。
func NewTheme(mode appearance.Mode) Theme {
	if os.Getenv("NO_COLOR") != "" {
		// 全局降级，保证 Theme 之外新建的样式（列表、帮助、bubbles 组件）也不输出颜色。
		lipgloss.SetColorProfile(termenv.Ascii)
		return plainTheme()
	}

	theme := appearance.Resolve(mode)
	if mode == appearance.System {
		if lipgloss.HasDarkBackground() {
			theme = appearance.ThemeDark
		} else {
			theme = appearance.ThemeLight
		}
	}
	if theme == appearance.ThemeLight {
		return lightTheme()
	}
	return darkTheme()
}

func darkTheme() Theme {
	accent := lipgloss.Color("39")
	dim := lipgloss.Color("245")
	fg := lipgloss.Color("252")

	return Theme{
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(accent).Bold(true),
		Tab:       lipgloss.NewStyle().Foreground(dim),
		TabActive: lipgloss.NewStyle().Foreground(fg).Bold(true),
		Header:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		Muted:     lipgloss.NewStyle().Foreground(dim),
		Body:      lipgloss.NewStyle().Foreground(fg),
		Preview:   lipgloss.NewStyle().Foreground(fg),
		StatusBar: lipgloss.NewStyle().Foreground(dim),
	}
}

// lightTheme 面向浅色终端背景：深灰正文、更深的主色，标题白字配蓝底。
func lightTheme() Theme {
	accent := lipgloss.Color("26")
	dim := lipgloss.Color("243")
	fg := lipgloss.Color("235")

	return Theme{
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(accent).Bold(true),
		Tab:       lipgloss.NewStyle().Foreground(dim),
		TabActive: lipgloss.NewStyle().Foreground(fg).Bold(true),
		Header:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		Muted:     lipgloss.NewStyle().Foreground(dim),
		Body:      lipgloss.NewStyle().Foreground(fg),
		Preview:   lipgloss.NewStyle().Foreground(fg),
		StatusBar: lipgloss.NewStyle().Foreground(dim),
	}
}

// plainTheme 供 NO_COLOR 环境使用：保留粗体层级，不输出任何颜色转义。
func plainTheme() Theme {
	plain := lipgloss.NewStyle()
	return Theme{
		Title:     plain.Bold(true),
		Tab:       plain,
		TabActive: plain.Bold(true),
		Header:    plain.Bold(true),
		Muted:     plain,
		Body:      plain,
		Preview:   plain,
		StatusBar: plain,
	}
}
```

- [ ] **Step 4: 接线 app.go 与 main.go**

在 `internal/ui/app.go`：import 增加 appearance；把 `NewModelWith` 里 `theme: NewTheme(),` 改为：

```go
		theme:  NewTheme(appearance.ParseMode(o.Config.Appearance.Mode)),
```

`Options` 结构体加字段：

```go
	// ThemeCacheDir 是主题注入文件目录（~/.kshell/cache/appearance），供启动 agent 时使用。
	ThemeCacheDir string
```

新增方法（放在 `resumeLaunch` 附近）：

```go
// themeOptions 组装当前颜色模式与主题缓存目录，供 launch 注入。
func (m Model) themeOptions() launch.ThemeOptions {
	return launch.ThemeOptions{
		Mode:     appearance.ParseMode(m.opts.Config.Appearance.Mode),
		CacheDir: m.opts.ThemeCacheDir,
	}
}
```

把 `resumeLaunch` 和 `newSessionLaunch` 的调用改为：

```go
	return launch.ForSession(m.opts.Providers, m.tools, s, m.themeOptions())
```

```go
	return launch.ForWorkspace(m.opts.Providers, m.tools, ws, m.themeOptions())
```

在 `cmd/kshell/main.go` 的 `ui.Options{...}` 增加：

```go
		ThemeCacheDir: paths.CacheAppearance,
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/ui/ ./cmd/kshell/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/ cmd/kshell/
git commit -m "feat(tui): 浅/深双调色板并跟随颜色模式"
```

---

## Task 7: 桌面绑定、事件与原生窗口

**Files:**
- Modify: `internal/desktop/app.go`, `internal/desktop/terminal.go`, `internal/desktop/app_test.go`
- Create: `internal/desktop/appearance.go`, `internal/desktop/appearance_windows.go`, `internal/desktop/appearance_other.go`, `internal/desktop/appearance_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/desktop/appearance_test.go`：

```go
package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
)

func TestGetAppearanceDefaultsToSystem(t *testing.T) {
	a := NewAppWith(Options{Config: config.Default()})
	got := a.GetAppearance()
	if got.Mode != "system" {
		t.Fatalf("mode = %q, want system", got.Mode)
	}
	if got.Resolved != string(appearance.Resolve(appearance.System)) {
		t.Fatalf("resolved = %q", got.Resolved)
	}
}

func TestSetAppearanceModePersistsAndEmits(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	var events []string
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(name string, _ ...any) { events = append(events, name) },
	})

	if err := a.SetAppearanceMode("dark"); err != nil {
		t.Fatalf("SetAppearanceMode: %v", err)
	}
	if a.GetAppearance().Mode != "dark" {
		t.Fatalf("mode = %q", a.GetAppearance().Mode)
	}
	if len(events) == 0 || events[len(events)-1] != "appearance:changed" {
		t.Fatalf("events = %v", events)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Appearance.Mode != "dark" {
		t.Fatalf("persisted = %q", loaded.Appearance.Mode)
	}

	if err := a.SetAppearanceMode("bogus"); err != nil {
		t.Fatalf("SetAppearanceMode(bogus): %v", err)
	}
	if a.GetAppearance().Mode != "system" {
		t.Fatalf("invalid mode should fall back to system, got %q", a.GetAppearance().Mode)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/desktop/ -run TestGetAppearance|TestSetAppearanceMode -count=1`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 扩展 App 与 Options**

在 `internal/desktop/app.go`：import 增加 `"github.com/yangk/kshell/internal/appearance"`。

`Options` 增加：

```go
	// Layout 是 ~/.kshell 的路径布局，写回配置（颜色模式）时使用。
	Layout config.Layout
	// ThemeCacheDir 是主题注入文件目录，供启动 agent 时使用。
	ThemeCacheDir string
```

`App` 结构体增加：

```go
	appearanceCancel context.CancelFunc // system 模式下的明暗监听取消函数
```

`initRealDeps` 里（在 `a.opts.Projects` 赋值段附近）加：

```go
	if a.opts.Layout.Config == "" {
		a.opts.Layout = paths
	}
	if a.opts.ThemeCacheDir == "" {
		a.opts.ThemeCacheDir = paths.CacheAppearance
	}
```

`Startup` 里在 `go a.reapLoop()` 后加：

```go
	a.emitAppearance()
	a.restartAppearanceWatcher()
```

新增方法（放在 `snapshot` 附近）：

```go
// themeOptions 组装当前颜色模式与主题缓存目录，供 launch 注入。
func (a *App) themeOptions() launch.ThemeOptions {
	o := a.snapshot()
	return launch.ThemeOptions{
		Mode:     appearance.ParseMode(o.Config.Appearance.Mode),
		CacheDir: o.ThemeCacheDir,
	}
}
```

把 `internal/desktop/app.go` 第 469 行的 `launch.ForSession(o.Providers, tools, s)` 改为：

```go
	l, err := launch.ForSession(o.Providers, tools, s, a.themeOptions())
```

- [ ] **Step 4: 扩展 terminal.go**

在 `internal/desktop/terminal.go`：

两处 `launch.ForSession/ForWorkspaceTool` 调用补 `a.themeOptions()`：

```go
	l, err := launch.ForSession(o.Providers, tools, s, a.themeOptions())
```

```go
	l, err := launch.ForWorkspaceTool(o.Providers, tools, ws, toolID, a.themeOptions())
```

两处 `terminal.Spec{...}` 补 `Env`：

```go
	}, terminal.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, cols, rows)
```

`NewSessionWithTool` 同样补参数。

- [ ] **Step 5: 外部窗口脚本注入 $env:**

把 `internal/desktop/app.go` 的 `launchWindow` 与 `psStatement` 改为：

```go
func (a *App) launchWindow(wm *WindowManager, l providers.Launch, titleText string) error {
	if wm == nil {
		return errNotReady
	}
	return wm.LaunchSession(l.Dir, titleText, psStatement(l.Path, l.Args, l.Env))
}

// psStatement 生成在已打开的 PowerShell 窗口里执行 CLI 的语句：先设 $env: 变量，再
// & 'bin' 'arg1'（调用运算符 + 单引号字面量，内嵌单引号双写转义）。
func psStatement(bin string, args []string, env map[string]string) string {
	s := ""
	for _, kv := range providers.EnvList(env) {
		i := strings.IndexByte(kv, '=')
		s += "$env:" + kv[:i] + " = " + psQuote(kv[i+1:]) + "; "
	}
	s += "& " + psQuote(bin)
	for _, arg := range args {
		s += " " + psQuote(arg)
	}
	return s
}
```

import 增加 `"strings"`（若未引入）。

在 `internal/desktop/app_test.go` 更新 `TestPsStatementQuotesArgs`：

```go
func TestPsStatementQuotesArgs(t *testing.T) {
	got := psStatement("claude", []string{"--resume", "it's s1"}, nil)
	want := `& 'claude' '--resume' 'it''s s1'`
	if got != want {
		t.Fatalf("psStatement = %q, want %q", got, want)
	}
}
```

并新增：

```go
func TestPsStatementInjectsEnv(t *testing.T) {
	got := psStatement("claude", nil, map[string]string{"COLORFGBG": "15;0"})
	want := `$env:COLORFGBG = '15;0'; & 'claude'`
	if got != want {
		t.Fatalf("psStatement = %q, want %q", got, want)
	}
}
```

- [ ] **Step 6: 新增 appearance 绑定**

创建 `internal/desktop/appearance.go`：

```go
package desktop

import (
	"context"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
)

// AppearanceInfo 是暴露给前端的颜色模式快照。
type AppearanceInfo struct {
	Mode     string `json:"mode"`
	Resolved string `json:"resolved"`
}

func (a *App) currentAppearance() AppearanceInfo {
	mode := appearance.ParseMode(a.snapshot().Config.Appearance.Mode)
	return AppearanceInfo{Mode: string(mode), Resolved: string(appearance.Resolve(mode))}
}

// GetAppearance 返回当前模式与解析后的明暗（light/dark）。
func (a *App) GetAppearance() AppearanceInfo { return a.currentAppearance() }

// SetAppearanceMode 更新颜色模式并持久化，随后广播事件并重启系统监听。
func (a *App) SetAppearanceMode(mode string) error {
	m := appearance.ParseMode(mode)

	a.mu.Lock()
	if a.opts.Layout.Config == "" {
		a.mu.Unlock()
		return errNotReady
	}
	a.opts.Config.Appearance.Mode = string(m)
	cfg := a.opts.Config
	layout := a.opts.Layout
	a.mu.Unlock()

	if err := config.Save(layout, cfg); err != nil {
		return err
	}
	a.restartAppearanceWatcher()
	a.emitAppearance()
	return nil
}

// restartAppearanceWatcher 仅在 system 模式下启动注册表轮询；其它模式停掉。
func (a *App) restartAppearanceWatcher() {
	a.mu.Lock()
	if a.appearanceCancel != nil {
		a.appearanceCancel()
		a.appearanceCancel = nil
	}
	mode := appearance.ParseMode(a.opts.Config.Appearance.Mode)
	a.mu.Unlock()

	if mode != appearance.System {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.appearanceCancel = cancel
	a.mu.Unlock()
	go appearance.NewWatcher(func(appearance.Theme) { a.emitAppearance() }).Run(ctx)
}

// emitAppearance 把当前明暗推给原生窗口与前端。
func (a *App) emitAppearance() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()

	info := a.currentAppearance()
	applyNativeTheme(ctx, info.Resolved)
	a.Emit("appearance:changed", info)
}
```

- [ ] **Step 7: 原生窗口主题**

创建 `internal/desktop/appearance_windows.go`：

```go
//go:build windows

package desktop

import (
	"context"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

var (
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const dwmwaUseImmersiveDarkMode = 20

// applyNativeTheme 让窗口边框/阴影匹配明暗，并设置窗口背景色避免切换瞬间闪烁。
// 主窗口标题固定为 "kshell"（main.go），用既有 findWindowByTitle 取句柄。
func applyNativeTheme(ctx context.Context, resolved string) {
	dark := resolved != "light"
	if hwnd := findWindowByTitle("kshell"); hwnd != 0 {
		v := int32(0)
		if dark {
			v = 1
		}
		procDwmSetWindowAttribute.Call(
			hwnd,
			uintptr(dwmwaUseImmersiveDarkMode),
			uintptr(unsafe.Pointer(&v)),
			unsafe.Sizeof(v),
		)
	}
	if ctx == nil {
		return
	}
	if dark {
		runtime.WindowSetBackgroundColour(ctx, 13, 17, 23, 255)
	} else {
		runtime.WindowSetBackgroundColour(ctx, 244, 246, 248, 255)
	}
}
```

创建 `internal/desktop/appearance_other.go`：

```go
//go:build !windows

package desktop

import "context"

// applyNativeTheme 非 Windows 无原生窗口着色实现。
func applyNativeTheme(context.Context, string) {}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `go build ./... ; go test ./internal/desktop/ -count=1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/desktop/
git commit -m "feat(desktop): 颜色模式绑定、事件与原生窗口主题"
```

---

## Task 8: 前端主题切换

**Files:**
- Modify: `frontend/src/style.css`, `frontend/index.html`, `frontend/src/lib/api.ts`, `frontend/src/state/store.ts`, `frontend/src/App.tsx`, `frontend/src/pages/Settings.tsx`, `frontend/src/components/TerminalView.tsx`, `frontend/src/pages/Settings.test.tsx`, `frontend/src/components/TerminalView.test.tsx`
- Create: `frontend/src/lib/appearance.ts`, `frontend/src/lib/appearance.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `frontend/src/lib/appearance.test.ts`：

```ts
import { describe, expect, it } from 'vitest';
import { terminalTheme } from './appearance';

describe('terminalTheme', () => {
  it('深色返回深底浅字', () => {
    expect(terminalTheme('dark')).toEqual({ background: '#0d1117', foreground: '#e6edf3' });
  });
  it('浅色返回白底深字', () => {
    expect(terminalTheme('light')).toEqual({ background: '#ffffff', foreground: '#1f2328' });
  });
});
```

在 `frontend/src/pages/Settings.test.tsx` 的 `mocks` 对象里补两个方法，并在 `beforeEach` 里设默认桩：

```ts
const mocks = vi.hoisted(() => ({
  getTools: vi.fn(),
  loadProvidersYAML: vi.fn(),
  saveProvidersYAML: vi.fn(),
  restartApp: vi.fn(),
  getAppearance: vi.fn(),
  setAppearanceMode: vi.fn(),
}));
```

```ts
  mocks.getAppearance.mockResolvedValue({ mode: 'system', resolved: 'dark' });
  mocks.setAppearanceMode.mockResolvedValue(undefined);
```

并新增用例：

```tsx
  it('外观选择调用 SetAppearanceMode', async () => {
    render(<Settings />);
    const darkBtn = await screen.findByRole('button', { name: '深色' });
    await act(async () => {
      fireEvent.click(darkBtn);
    });
    expect(mocks.setAppearanceMode).toHaveBeenCalledWith('dark');
  });
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run lib/appearance.test.ts pages/Settings.test.tsx`（在 `frontend/`）
Expected: FAIL（`./appearance` 不存在、mock 缺方法）

- [ ] **Step 3: 新增外观工具模块**

创建 `frontend/src/lib/appearance.ts`：

```ts
// 颜色模式相关的前端工具：类型与 xterm 主题映射。
export type AppearanceMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export interface AppearanceInfo {
  mode: AppearanceMode;
  resolved: ResolvedTheme;
}

// terminalTheme 按解析后的明暗返回 xterm 配色。
export function terminalTheme(theme: ResolvedTheme) {
  return theme === 'dark'
    ? { background: '#0d1117', foreground: '#e6edf3' }
    : { background: '#ffffff', foreground: '#1f2328' };
}
```

- [ ] **Step 4: api.ts 增加绑定**

在 `frontend/src/lib/api.ts`：import 增加

```ts
import type { AppearanceInfo } from './appearance';
```

`AppBindings` 接口加：

```ts
  GetAppearance(): Promise<AppearanceInfo>;
  SetAppearanceMode(mode: string): Promise<void>;
```

文件末尾加：

```ts
// ---- 颜色模式 ----

// getAppearance 返回当前模式与解析后的明暗；绑定不可用时返回跟随系统的兜底值。
export async function getAppearance(): Promise<AppearanceInfo> {
  const a = app();
  if (!a) return { mode: 'system', resolved: 'dark' };
  return a.GetAppearance();
}

// setAppearanceMode 设置颜色模式（写回 config.yaml），错误向上抛。
export async function setAppearanceMode(mode: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SetAppearanceMode(mode);
}

// onAppearanceChanged 订阅颜色模式/系统明暗变化，返回取消订阅函数。
export function onAppearanceChanged(cb: (info: AppearanceInfo) => void): () => void {
  return EventsOn('appearance:changed', (p: AppearanceInfo) =>
    cb({ mode: p?.mode ?? 'system', resolved: p?.resolved ?? 'dark' }),
  );
}
```

- [ ] **Step 5: store 保存外观状态**

在 `frontend/src/state/store.ts`：import 增加

```ts
import type { AppearanceInfo } from '../lib/appearance';
```

`AppState` 接口加：

```ts
  appearance: AppearanceInfo;
  setAppearance(info: AppearanceInfo): void;
```

实现里（`newSessionTool` 附近）加：

```ts
      appearance: { mode: 'system', resolved: 'dark' },
      setAppearance: (appearance) => set({ appearance }),
```

`partialize` 不加入 appearance（来自 Go 绑定，不持久化）。

- [ ] **Step 6: CSS 改为 data-theme 驱动**

编辑 `frontend/src/style.css`：

把 `:root { ...浅色 token... }` 的写法改为 `:root, [data-theme="light"] {`（内容不变），把 `@media (prefers-color-scheme: dark) { :root { ... } }` 改为：

```css
[data-theme="dark"] {
  color-scheme: dark;
  --background: #0d1117;  --foreground: #e6edf3;
  --card: #151b23;        --card-foreground: #e6edf3;
  --muted: #1f262e;       --muted-foreground: #8b949e;
  --border: #2d333b;      --input: #2d333b;
  --primary: #4493f8;     --primary-foreground: #0d1117;
  --secondary: #1f262e;   --secondary-foreground: #e6edf3;
  --accent: #101d2f;      --accent-foreground: #4493f8;
  --destructive: #e5534b; --destructive-foreground: #ffffff;
  --success: #3fb950;     --warning: #d29922;
  --ring: #4493f899;
}
```

并在浅色块加 `color-scheme: light;`；删除文件中原有的 `:root { color-scheme: light dark; }` 块。

- [ ] **Step 7: index.html 首屏防闪**

在 `frontend/index.html` 的 `<head>` 内 `<title>` 前加：

```html
  <script>
    (function () {
      try {
        var dark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
        document.documentElement.dataset.theme = dark ? 'dark' : 'light';
      } catch (e) {}
    })();
  </script>
```

- [ ] **Step 8: App.tsx 初始化与订阅**

在 `frontend/src/App.tsx`：import 增加

```ts
import { getAppearance, onAppearanceChanged } from './lib/api';
import type { AppearanceInfo } from './lib/appearance';
```

在组件内加 effect：

```tsx
  useEffect(() => {
    const apply = (info: AppearanceInfo) => {
      document.documentElement.dataset.theme = info.resolved;
      useAppStore.getState().setAppearance(info);
    };
    getAppearance().then(apply).catch(() => {});
    const off = onAppearanceChanged(apply);
    return off;
  }, []);
```

并更新 `frontend/src/App.test.tsx` 的 `mocks` 对象，补两个方法（`vi.mock('./lib/api', () => mocks)` 是整体替换，缺一即报错）：

```ts
  getAppearance: vi.fn(),
  onAppearanceChanged: vi.fn(),
```

在 `beforeEach` 里加默认桩：

```ts
  mocks.getAppearance.mockResolvedValue({ mode: 'system', resolved: 'dark' });
  mocks.onAppearanceChanged.mockReturnValue(() => {});
```

- [ ] **Step 9: Settings 增加外观选择**

在 `frontend/src/pages/Settings.tsx`：import 增加 `getAppearance, setAppearanceMode`；在组件顶部加状态：

```tsx
  const [appearance, setAppearanceLocal] = useState<string>('system');
```

在首个 `useEffect` 内加：

```tsx
    getAppearance()
      .then((info) => {
        if (!cancelled) setAppearanceLocal(info.mode);
      })
      .catch(() => {});
```

新增切换处理与 UI（放在「工具检测」section 之前）：

```tsx
  const handleAppearance = async (mode: string) => {
    try {
      await setAppearanceMode(mode);
      setAppearanceLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };
```

```tsx
        <section className="mb-5 rounded border border-border bg-card p-3.5">
          <h2 className="mb-3 text-sm font-medium">外观</h2>
          <div className="flex gap-2">
            {[
              { value: 'system', label: '跟随系统' },
              { value: 'light', label: '浅色' },
              { value: 'dark', label: '深色' },
            ].map((opt) => (
              <Button
                key={opt.value}
                variant={appearance === opt.value ? 'default' : 'secondary'}
                onClick={() => void handleAppearance(opt.value)}
              >
                {opt.label}
              </Button>
            ))}
          </div>
        </section>
```

- [ ] **Step 10: TerminalView 使用解析后明暗并热更新**

在 `frontend/src/components/TerminalView.tsx`：import 增加

```ts
import { terminalTheme } from '../lib/appearance';
import { useAppStore } from '../state/store';
```

删除本地 `terminalTheme()` 函数，改为读取 store：

```tsx
  const resolved = useAppStore((s) => s.appearance.resolved);
```

把 `theme: terminalTheme(),` 改为 `theme: terminalTheme(resolved),`（注意 `useEffect` 依赖仍只取 `termId`；这里首次渲染值即可）。在 `active` effect 之后加：

```tsx
  // 明暗变化时热更新已存在终端实例的配色
  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.theme = terminalTheme(resolved);
    }
  }, [resolved]);
```

并把 `frontend/src/components/TerminalView.test.tsx` 末尾依赖 `matchMedia` 的用例（`终端主题跟随系统深浅色…`）替换为 store 驱动：

```tsx
  it('终端主题跟随 store 的 resolved 明暗', () => {
    useAppStore.getState().setAppearance({ mode: 'dark', resolved: 'dark' });
    const dark = render(<TerminalView term={TERM} active />);
    expect(term(0).options.theme).toEqual({ background: '#0d1117', foreground: '#e6edf3' });
    dark.unmount();

    useAppStore.getState().setAppearance({ mode: 'light', resolved: 'light' });
    const light = render(<TerminalView term={TERM} active />);
    expect(term(1).options.theme).toEqual({ background: '#ffffff', foreground: '#1f2328' });
    light.unmount();
  });
```

（在该测试文件顶部 import 增加 `import { useAppStore } from '../state/store';`；无用的 `setMatchMedia` 辅助函数可一并删除。）

- [ ] **Step 11: 运行前端测试**

Run: `npm test -- --run`（在 `frontend/`）
Expected: PASS

- [ ] **Step 12: 类型与构建**

Run: `npm run build`（在 `frontend/`）
Expected: 成功

- [ ] **Step 13: Commit**

```bash
git add frontend/
git commit -m "feat(frontend): 外观设置与 data-theme 明暗切换"
```

---

## Task 9: 全量验证

- [ ] **Step 1: Go 编译与静态检查**

Run: `go build ./... ; go vet ./...`
Expected: 无输出（成功）

- [ ] **Step 2: Go 全量测试**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 3: 前端测试与构建**

Run（在 `frontend/`）: `npm test -- --run ; npm run build`
Expected: PASS + 构建成功

- [ ] **Step 4: 手动冒烟（Windows 桌面版）**

1. 启动桌面版，设置页「外观」切成「浅色」→ UI、内嵌终端、原生窗口边框即时变浅；重开应用后仍为浅色。
2. 切成「跟随系统」，在系统设置里切换深/浅色 → 桌面 UI 与内嵌终端跟随变化（约 3s 内）。
3. 新建一个 claude 会话（内嵌终端）→ 观察其 TUI 主题与 kshell 当前明暗一致。
4. 检查 `~/.kshell/config.yaml` 出现 `appearance: { mode: ... }`。
5. 确认未修改 `~/.claude/settings.json`、`~/.codex/config.toml` 等工具自身配置。

- [ ] **Step 5: Commit（如有修正）**

```bash
git add -A
git commit -m "test: 颜色模式全量验证与冒烟修正"
```

---

## 自审记录

- **Spec 覆盖**：模式配置(Task 3)、OS 探测(Task 2)、桌面三态入口(Task 7/8)、TUI(Task 6)、内嵌终端(Task 8)、Wails 窗口(Task 7)、agent 注入全工具(Task 4/5)、实时切换(Task 7 事件 + Task 8 订阅)、YAML 持久化(Task 3/7) —— 均有任务对应。
- **命名一致性**：`appearance.Mode/Theme`、`ThemeOptions{Mode,CacheDir}`、`providers.Themer.ThemeOverrides(theme, cacheDir)`、`AppearanceInfo{mode,resolved}` 前后一致。
- **偏离说明**：spec 第 4.2 节提到 `DwmSetWindowAttribute`，Task 7 通过主窗口标题 `"kshell"` 取 HWND 实现；若该标题在运行期被修改则退化为仅设背景色，不影响前端主题。
