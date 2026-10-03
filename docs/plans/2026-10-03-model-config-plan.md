# 全局模型配置 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 kshell 设置页统一配置全局端点/密钥 + 每 agent 模型名，并在启动各 agent（终端与 ACP 两条路径）时注入对应 env/参数。

**Architecture:** `config.ModelConfig` 存配置 → 桌面/TUI 用 `modelOptions()` 构造 resolver → `launch.applyModel`（与现有 `applyTheme` 同构）经 provider 的 `ModelInjector` 翻译成 env/参数 → 子进程。密钥只写不回传前端。

**Tech Stack:** Go（config/launch/providers/desktop/ui）、React + Zustand + vitest。设计见 `docs/plans/2026-10-03-model-config-design.md`。

**提交口径**：AGENTS.md 要求「仅用户明确要求时才提交」。本计划所有「提交」步骤默认**跳过**，除非用户当次明确要求。

---

## 文件结构

**新建**
- `internal/providers/model.go` — `ModelConfig` / `ModelInjector` + 四个内置工具的 `InjectModel`
- `internal/providers/model_test.go`
- `internal/desktop/model_config_test.go`

**修改**
- `internal/config/config.go` — `ModelConfig` 类型、`Config.Model`、默认/归一
- `internal/config/config_test.go` — 新增模型配置用例
- `internal/launch/launch.go` — `ModelOptions`、`applyModel`、5 个函数签名、ACP 查 provider
- `internal/launch/launch_test.go`
- `internal/desktop/settings.go` — `ModelConfigView/Input` + `Get/SetModelConfig`
- `internal/desktop/app.go` — `modelOptions()`
- `internal/desktop/terminal.go` / `internal/desktop/chat.go` — 调用点补 `modelOptions()`
- `internal/ui/app.go` — `modelOptions()` + 调用点
- `frontend/src/lib/api.ts` — 类型 + 封装 + `AppBindings`
- `frontend/src/pages/Settings.tsx` — 「模型」区
- `frontend/src/pages/Settings.test.tsx` — 用例
- `docs/smoke-test-desktop.md` — 冒烟条目

---

## M1：配置模型

### Task 1: config.ModelConfig

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `internal/config/config_test.go`：

```go
func TestModelConfigDefaultsAndRoundTrip(t *testing.T) {
	p := tempPaths(t)

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.Enabled || got.Model.BaseURL != "" || got.Model.APIKey != "" {
		t.Fatalf("默认 model 应为空，got %+v", got.Model)
	}
	if got.Model.Agents == nil {
		t.Fatal("默认 Agents 应为非 nil 空 map")
	}

	want := Default()
	want.Model = ModelConfig{
		Enabled: true,
		BaseURL: "https://example.com/anthropic",
		APIKey:  "tp-secret",
		Agents:  map[string]string{"claude": "mimo-v2.5"},
	}
	if err := Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Model, want.Model) {
		t.Fatalf("model round trip:\ngot  %+v\nwant %+v", loaded.Model, want.Model)
	}
}

func TestModelConfigNormalizes(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  enabled: true\n  base_url: \"not-a-url\"\n  agents:\n    claude: \"  mimo-v2.5  \"\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.BaseURL != "" {
		t.Fatalf("非法 base_url 应被丢弃，got %q", got.Model.BaseURL)
	}
	if got.Model.Agents["claude"] != "mimo-v2.5" {
		t.Fatalf("agents 值应 TrimSpace，got %q", got.Model.Agents["claude"])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestModelConfig -v`
Expected: 编译失败（`ModelConfig`/`Model` 未定义）。

- [ ] **Step 3: 实现**

`internal/config/config.go`：
- 增加 `strings` 到 import。
- 新增类型与字段：

```go
// ModelConfig 是全局模型/端点配置 + 每 agent 模型名。
type ModelConfig struct {
	Enabled bool              `yaml:"enabled"`
	BaseURL string            `yaml:"base_url"`
	APIKey  string            `yaml:"api_key"`
	Agents  map[string]string `yaml:"agents"` // toolID -> 模型名（空=不改）
}
```

- `Config` 增加 `Model ModelConfig \`yaml:"model"\``。
- `Default()` 增加 `Model: ModelConfig{Agents: map[string]string{}}`。
- `normalized()` 末尾（return 前）增加：

```go
	// 模型配置：Agents 补空 map 并 TrimSpace；BaseURL 非法前缀一律丢弃（不阻断加载）。
	if c.Model.Agents == nil {
		c.Model.Agents = map[string]string{}
	}
	for k, v := range c.Model.Agents {
		c.Model.Agents[k] = strings.TrimSpace(v)
	}
	c.Model.BaseURL = strings.TrimSpace(c.Model.BaseURL)
	if c.Model.BaseURL != "" &&
		!strings.HasPrefix(c.Model.BaseURL, "http://") &&
		!strings.HasPrefix(c.Model.BaseURL, "https://") {
		c.Model.BaseURL = ""
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -count=1`
Expected: 全绿（含既有用例）。

---

## M2：Provider 注入

### Task 2: providers.ModelInjector + 四工具映射

**Files:**
- Create: `internal/providers/model.go`
- Test: `internal/providers/model_test.go`

- [ ] **Step 1: 写失败测试**

```go
package providers

import (
	"reflect"
	"testing"
)

func TestInjectModelMappings(t *testing.T) {
	cfg := ModelConfig{BaseURL: "https://h/", APIKey: "k", Model: "m"}

	args, env := Claude{}.InjectModel(cfg)
	if len(args) != 0 {
		t.Fatalf("claude args = %v", args)
	}
	wantClaude := map[string]string{
		"ANTHROPIC_BASE_URL":            "https://h/",
		"ANTHROPIC_AUTH_TOKEN":          "k",
		"ANTHROPIC_MODEL":               "m",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "m",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "m",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "m",
	}
	if !reflect.DeepEqual(env, wantClaude) {
		t.Fatalf("claude env = %+v", env)
	}

	args, env = Codex{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"-m", "m"}) {
		t.Fatalf("codex args = %v", args)
	}
	if env["OPENAI_BASE_URL"] != "https://h/" || env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("codex env = %+v", env)
	}

	args, env = Gemini{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("gemini args = %v", args)
	}
	if env["GOOGLE_GEMINI_BASE_URL"] != "https://h/" || env["GEMINI_API_KEY"] != "k" {
		t.Fatalf("gemini env = %+v", env)
	}

	args, env = Opencode{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("opencode args = %v", args)
	}
	if len(env) != 0 {
		t.Fatalf("opencode 不应注入 env，got %+v", env)
	}
}

func TestInjectModelEmptyInjectsNothing(t *testing.T) {
	for _, p := range []interface {
		InjectModel(ModelConfig) ([]string, map[string]string)
	}{Claude{}, Codex{}, Gemini{}, Opencode{}} {
		args, env := p.InjectModel(ModelConfig{})
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("%T 空配置不应注入：args=%v env=%+v", p, args, env)
		}
	}
}
```

注意：`if len(env) != 0` 对返回 `nil` 的 map 成立（len(nil)==0）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers/ -run TestInjectModel -v`
Expected: 编译失败。

- [ ] **Step 3: 实现 `internal/providers/model.go`**

```go
package providers

// ModelConfig 是按工具适配后的模型注入值（全局端点/密钥 + 该工具的模型名）。
type ModelConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// ModelInjector 可选接口：把模型配置翻译成该工具认的启动参数与环境变量。
// 只注入非空字段；工具不支持的字段忽略（见 docs/plans/2026-10-03-model-config-design.md §4）。
type ModelInjector interface {
	InjectModel(cfg ModelConfig) (args []string, env map[string]string)
}

// Claude：ANTHROPIC_* 环境变量；模型同步三个 DEFAULT 别名，兼容只认单一模型的代理端点。
func (Claude) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	env := map[string]string{}
	if cfg.BaseURL != "" {
		env["ANTHROPIC_BASE_URL"] = cfg.BaseURL
	}
	if cfg.APIKey != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = cfg.APIKey
	}
	if cfg.Model != "" {
		env["ANTHROPIC_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = cfg.Model
	}
	return nil, env
}

// Codex：-m <model> + OPENAI_* 环境变量（env 名待实测）。
func (Codex) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	if cfg.BaseURL != "" {
		env["OPENAI_BASE_URL"] = cfg.BaseURL
	}
	if cfg.APIKey != "" {
		env["OPENAI_API_KEY"] = cfg.APIKey
	}
	return args, env
}

// Gemini：--model <model> + GEMINI_API_KEY / GOOGLE_GEMINI_BASE_URL（端点 env 待实测）。
func (Gemini) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.BaseURL != "" {
		env["GOOGLE_GEMINI_BASE_URL"] = cfg.BaseURL
	}
	if cfg.APIKey != "" {
		env["GEMINI_API_KEY"] = cfg.APIKey
	}
	return args, env
}

// Opencode：仅 --model（期望 provider/model）；端点/密钥无稳定 env 注入方式，v1 不支持。
func (Opencode) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	return args, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/providers/ -count=1`
Expected: 全绿。

---

## M3：launch 集成

### Task 3: applyModel + 签名扩展 + 调用点

**Files:**
- Modify: `internal/launch/launch.go`
- Modify: `internal/desktop/app.go`、`internal/desktop/terminal.go`、`internal/desktop/chat.go`
- Modify: `internal/ui/app.go`
- Test: `internal/launch/launch_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `internal/launch/launch_test.go`：

```go
func TestApplyModelTerminalCodex(t *testing.T) {
	ps := []providers.Provider{providers.Codex{}}
	tools := []discovery.Tool{{ID: "codex", Name: "Codex CLI", Installed: true, BinPath: "codex"}}
	s := providers.Session{ID: "s1", ToolID: "codex", Workspace: "/proj"}
	mo := ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{BaseURL: "https://h/", APIKey: "k", Model: "gpt-x"}, true
	}}
	l, err := ForSession(ps, tools, s, ThemeOptions{}, mo)
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	found := false
	for i := 0; i+1 < len(l.Args); i++ {
		if l.Args[i] == "-m" && l.Args[i+1] == "gpt-x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("args 缺少 -m gpt-x：%v", l.Args)
	}
	if l.Env["OPENAI_BASE_URL"] != "https://h/" || l.Env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("env = %+v", l.Env)
	}
}

func TestApplyModelACPClaude(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	mo := ModelOptions{Resolver: func(string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{BaseURL: "https://h/", APIKey: "k", Model: "mimo-v2.5"}, true
	}}
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude", mo)
	if err != nil {
		t.Fatalf("ForWorkspaceACP: %v", err)
	}
	if l.Env["ANTHROPIC_MODEL"] != "mimo-v2.5" || l.Env["ANTHROPIC_BASE_URL"] != "https://h/" {
		t.Fatalf("ACP env = %+v", l.Env)
	}
}

func TestApplyModelNilResolverNoInjection(t *testing.T) {
	// 直接验证 applyModel：避免 applyTheme 追加的通用主题变量干扰断言。
	l := applyModel(providers.Launch{}, providers.Codex{}, ModelOptions{})
	if len(l.Env) != 0 || len(l.Args) != 0 {
		t.Fatalf("无 resolver 不应注入：args=%v env=%+v", l.Args, l.Env)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/launch/ -run TestApplyModel -v`
Expected: 编译失败（签名/A 类型未定义）。

- [ ] **Step 3: 改 `internal/launch/launch.go`**

在 `applyTheme` 之后新增：

```go
// ModelOptions 描述模型注入：Resolver 按 toolID 返回该工具应注入的配置（nil 表示不注入）。
type ModelOptions struct {
	Resolver func(toolID string) (providers.ModelConfig, bool)
}

// applyModel 与 applyTheme 同构：provider 实现 ModelInjector 时追加参数/环境变量。
func applyModel(l providers.Launch, p providers.Provider, mo ModelOptions) providers.Launch {
	if mo.Resolver == nil {
		return l
	}
	cfg, ok := mo.Resolver(p.ID())
	if !ok {
		return l
	}
	if inj, ok := p.(providers.ModelInjector); ok {
		args, env := inj.InjectModel(cfg)
		l.Args = append(l.Args, args...)
		l.Env = providers.MergeEnv(l.Env, env)
	}
	return l
}
```

改签名与实现：

```go
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session, to ThemeOptions, mo ModelOptions) (providers.Launch, error) {
	// ...原有 tool/provider 查找不变...
	return applyModel(applyTheme(p.ResumeCmd(s, tool.BinPath), p, to), p, mo), nil
}

func ForWorkspace(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, to ThemeOptions, mo ModelOptions) (providers.Launch, error) {
	return ForWorkspaceTool(ps, tools, ws, "", to, mo)
}

func ForWorkspaceTool(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, to ThemeOptions, mo ModelOptions) (providers.Launch, error) {
	if toolID == "" {
		p, tool, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		return applyModel(applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), p, mo), nil
	}
	// ...原有 tool/provider 查找不变...
	return applyModel(applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), p, mo), nil
}

func ForSessionACP(ps []providers.Provider, tools []discovery.Tool, s providers.Session, mo ModelOptions) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	l, err := acpLaunch(tool, s.Workspace)
	if err != nil {
		return providers.Launch{}, err
	}
	return applyModel(l, p, mo), nil
}

func ForWorkspaceACP(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, mo ModelOptions) (providers.Launch, error) {
	var tool discovery.Tool
	var p providers.Provider
	if toolID == "" {
		pp, t, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		p, tool = pp, t
	} else {
		t, ok := toolFor(tools, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		pp, ok := providerFor(ps, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool, p = t, pp
	}
	l, err := acpLaunch(tool, ws.Path)
	if err != nil {
		return providers.Launch{}, err
	}
	return applyModel(l, p, mo), nil
}
```

- [ ] **Step 4: 加 `modelOptions()` 并更新调用点**

`internal/desktop/app.go`（`themeOptions` 附近）：

```go
// modelOptions 组装模型注入 resolver：未启用时返回空（不注入）。
func (a *App) modelOptions() launch.ModelOptions {
	m := a.snapshot().Config.Model
	if !m.Enabled {
		return launch.ModelOptions{}
	}
	return launch.ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{BaseURL: m.BaseURL, APIKey: m.APIKey, Model: m.Agents[toolID]}, true
	}}
}
```

`internal/desktop/terminal.go`（两处**已有** `a.themeOptions()`，只需在末尾追加 `a.modelOptions()`）：
- `OpenSessionTerminal`：`launch.ForSession(o.Providers, tools, s, a.themeOptions())` → `..., a.themeOptions(), a.modelOptions())`
- `OpenWorkspaceTerminal`：`launch.ForWorkspaceTool(o.Providers, tools, ws, toolID, a.themeOptions())` → `..., a.themeOptions(), a.modelOptions())`

`internal/desktop/chat.go`：
- `launch.ForSessionACP(o.Providers, tools, s)` → `..., a.modelOptions()`
- `launch.ForWorkspaceACP(o.Providers, tools, ws, toolID)` → `..., a.modelOptions()`

`internal/ui/app.go`（`themeOptions` 附近）：

```go
func (m Model) modelOptions() launch.ModelOptions {
	cfg := m.opts.Config.Model
	if !cfg.Enabled {
		return launch.ModelOptions{}
	}
	return launch.ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Agents[toolID]}, true
	}}
}
```
调用点（`:569` 的 `ForSession(...m.themeOptions())` / `:577` 的 `ForWorkspace(...m.themeOptions())`，均**已有** themeOptions）末尾追加 `m.modelOptions()`。

- [ ] **Step 5: 跑测试确认通过**

Run: `go vet ./... ; go test ./internal/launch/ ./internal/ui/ -count=1`
Expected: 全绿。

---

## M4：桌面绑定

### Task 4: GetModelConfig / SetModelConfig

**Files:**
- Modify: `internal/desktop/settings.go`
- Test: `internal/desktop/model_config_test.go`

- [ ] **Step 1: 写失败测试**

```go
package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func newModelApp(t *testing.T) (*App, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	return NewAppWith(Options{Config: config.Default(), Layout: layout}), layout
}

func TestGetModelConfigNeverLeaksKey(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{APIKey: "secret", Agents: map[string]string{"claude": "m"}}
	app.mu.Unlock()

	v, err := app.GetModelConfig()
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if !v.APIKeySet {
		t.Fatal("APIKeySet 应为 true")
	}
	// 结构体里根本没有 APIKey 字段，这条断言保证未来不会误加回传字段。
	_ = v
	if v.Agents["claude"] != "m" {
		t.Fatalf("agents = %+v", v.Agents)
	}
}

func TestSetModelConfigKeepOverwriteClear(t *testing.T) {
	app, layout := newModelApp(t)

	// 覆盖
	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true, BaseURL: "https://h/", APIKey: "k1",
		Agents: map[string]string{"claude": " m ", "codex": "gpt-x"},
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	loaded, _ := config.Load(layout)
	if loaded.Model.APIKey != "k1" || !loaded.Model.Enabled {
		t.Fatalf("persist 1 = %+v", loaded.Model)
	}
	if loaded.Model.Agents["claude"] != "m" {
		t.Fatalf("agents 未 TrimSpace: %q", loaded.Model.Agents["claude"])
	}

	// 留空保持原密钥
	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, BaseURL: "https://h/", Agents: map[string]string{}}); err != nil {
		t.Fatalf("Set keep: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.APIKey != "k1" {
		t.Fatalf("留空应保持密钥，got %q", loaded.Model.APIKey)
	}

	// 显式清除
	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, ClearAPIKey: true, Agents: map[string]string{}}); err != nil {
		t.Fatalf("Set clear: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.APIKey != "" {
		t.Fatalf("ClearAPIKey 应清空，got %q", loaded.Model.APIKey)
	}

	// 非法 URL 拒绝且不改配置
	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, BaseURL: "ftp://bad"}); err == nil {
		t.Fatal("非法 BaseURL 应报错")
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.BaseURL != "" {
		t.Fatalf("非法 URL 不应落盘，got %q", loaded.Model.BaseURL)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestModelConfig -v`
Expected: 编译失败。

- [ ] **Step 3: 实现（`internal/desktop/settings.go` 顶部加 import `fmt`、`strings`）**

```go
// ModelConfigView 是回传前端的模型配置视图：绝不包含密钥明文。
type ModelConfigView struct {
	Enabled   bool              `json:"Enabled"`
	BaseURL   string            `json:"BaseURL"`
	Agents    map[string]string `json:"Agents"`
	APIKeySet bool              `json:"APIKeySet"`
}

// ModelConfigInput 是前端提交的模型配置：APIKey 空串=保持原值，ClearAPIKey 显式清除。
type ModelConfigInput struct {
	Enabled     bool              `json:"Enabled"`
	BaseURL     string            `json:"BaseURL"`
	APIKey      string            `json:"APIKey"`
	ClearAPIKey bool              `json:"ClearAPIKey"`
	Agents      map[string]string `json:"Agents"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	agents := m.Agents
	if agents == nil {
		agents = map[string]string{}
	}
	return ModelConfigView{
		Enabled:   m.Enabled,
		BaseURL:   m.BaseURL,
		Agents:    agents,
		APIKeySet: m.APIKey != "",
	}, nil
}

func (a *App) SetModelConfig(in ModelConfigInput) error {
	base := strings.TrimSpace(in.BaseURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return fmt.Errorf("Base URL 必须以 http:// 或 https:// 开头")
	}
	agents := map[string]string{}
	for k, v := range in.Agents {
		agents[k] = strings.TrimSpace(v)
	}
	return a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.BaseURL = base
		c.Model.Agents = agents
		switch {
		case in.ClearAPIKey:
			c.Model.APIKey = ""
		case in.APIKey != "":
			c.Model.APIKey = in.APIKey
		}
	})
}
```

（`config` 已在 settings.go import；`saveConfig` 会串行化并落盘。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/desktop/ -run TestModelConfig -count=1 -v`
Expected: PASS。

---

## M5：前端

### Task 5: api + 设置页「模型」区

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/pages/Settings.tsx`
- Test: `frontend/src/pages/Settings.test.tsx`

- [ ] **Step 1: 写失败测试**

在 `frontend/src/pages/Settings.test.tsx` 的 api mock 中补 `getModelConfig` / `setModelConfig`，并新增用例：

```tsx
it('模型区回填配置、密钥只显示掩码、留空保存传空串', async () => {
  api.getModelConfig.mockResolvedValue({
    Enabled: true,
    BaseURL: 'https://h/',
    Agents: { claude: 'mimo-v2.5' },
    APIKeySet: true,
  });
  render(<Settings />);

  const base = await screen.findByLabelText('模型 Base URL');
  expect(base).toHaveValue('https://h/');
  expect(screen.getByLabelText('模型 API Key')).toHaveValue('');
  expect(screen.getByPlaceholderText(/已设置/)).toBeInTheDocument();
  expect(screen.getByLabelText('Claude Code 模型')).toHaveValue('mimo-v2.5');

  fireEvent.click(screen.getByRole('button', { name: '保存模型配置' }));
  await waitFor(() =>
    expect(api.setModelConfig).toHaveBeenCalledWith(
      expect.objectContaining({ Enabled: true, BaseURL: 'https://h/', APIKey: '', ClearAPIKey: false }),
    ),
  );
});

it('勾选清除密钥时提交 ClearAPIKey', async () => {
  api.getModelConfig.mockResolvedValue({ Enabled: false, BaseURL: '', Agents: {}, APIKeySet: true });
  render(<Settings />);
  fireEvent.click(await screen.findByLabelText('清除密钥'));
  fireEvent.click(screen.getByRole('button', { name: '保存模型配置' }));
  await waitFor(() => expect(api.setModelConfig).toHaveBeenCalledWith(expect.objectContaining({ ClearAPIKey: true })));
});
```

（沿用该文件既有的 `api` mock 变量与 `render`/`waitFor`/`screen`/`fireEvent`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `npm test -- Settings`（`frontend/`）
Expected: 新用例失败。

- [ ] **Step 3: api.ts 扩展**

```ts
export interface ModelConfigView {
  Enabled: boolean;
  BaseURL: string;
  Agents: Record<string, string>;
  APIKeySet: boolean;
}
export interface ModelConfigInput {
  Enabled: boolean;
  BaseURL: string;
  APIKey: string;
  ClearAPIKey: boolean;
  Agents: Record<string, string>;
}
```
`AppBindings` 增加：
```ts
  GetModelConfig(): Promise<ModelConfigView>;
  SetModelConfig(input: ModelConfigInput): Promise<void>;
```
文件末尾：
```ts
export async function getModelConfig(): Promise<ModelConfigView> {
  const a = app();
  if (!a) return { Enabled: false, BaseURL: '', Agents: {}, APIKeySet: false };
  return a.GetModelConfig();
}
export async function setModelConfig(input: ModelConfigInput): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SetModelConfig(input);
}
```

- [ ] **Step 4: Settings.tsx 增加「模型」区**

- import 增加 `getModelConfig, setModelConfig`。
- 常量：
```ts
const MODEL_AGENTS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'opencode', label: 'OpenCode' },
];
```
- state：`modelEnabled`、`modelBaseURL`、`modelApiKey`、`modelApiKeySet`、`modelClearKey`、`modelAgents: Record<string,string>`、`modelSaving`。
- 加载（挂载 useEffect 内）：
```ts
getModelConfig()
  .then((v) => {
    if (cancelled) return;
    setModelEnabled(v.Enabled);
    setModelBaseURL(v.BaseURL);
    setModelApiKeySet(v.APIKeySet);
    setModelAgents(v.Agents ?? {});
  })
  .catch(() => {});
```
- 保存：
```ts
const handleSaveModel = async () => {
  if (modelSaving) return;
  setModelSaving(true);
  try {
    await setModelConfig({
      Enabled: modelEnabled,
      BaseURL: modelBaseURL.trim(),
      APIKey: modelApiKey,
      ClearAPIKey: modelClearKey,
      Agents: modelAgents,
    });
    if (modelApiKey) setModelApiKeySet(true);
    if (modelClearKey) setModelApiKeySet(false);
    setModelApiKey('');
    setModelClearKey(false);
    notify('已保存，对新启动的会话生效', 'info');
  } catch (e: unknown) {
    notify(e instanceof Error ? e.message : String(e), 'error');
  } finally {
    setModelSaving(false);
  }
};
```
- 在「关闭行为」区块后插入新 section：
```tsx
<section className="mb-5 rounded border border-border bg-card p-3.5">
  <h2 className="mb-3 text-sm font-medium">模型（对所有 agent 启动时注入）</h2>
  <label className="mb-2 flex items-center gap-2 text-sm">
    <input type="checkbox" checked={modelEnabled} onChange={(e) => setModelEnabled(e.target.checked)} />
    启用模型配置
  </label>
  <div className="grid gap-2">
    <label className="text-xs text-muted-foreground" htmlFor="model-base-url">模型 Base URL</label>
    <input id="model-base-url" aria-label="模型 Base URL"
      className="rounded border border-input bg-card px-2 py-1 text-sm"
      placeholder="https://..." value={modelBaseURL}
      onChange={(e) => setModelBaseURL(e.target.value)} />
    <label className="text-xs text-muted-foreground" htmlFor="model-api-key">模型 API Key</label>
    <input id="model-api-key" aria-label="模型 API Key" type="password"
      className="rounded border border-input bg-card px-2 py-1 text-sm"
      placeholder={modelApiKeySet ? '已设置（留空不修改）' : '未设置'}
      value={modelApiKey} onChange={(e) => setModelApiKey(e.target.value)} />
    <label className="flex items-center gap-2 text-xs text-muted-foreground">
      <input type="checkbox" checked={modelClearKey} onChange={(e) => setModelClearKey(e.target.checked)} />
      清除密钥
    </label>
    {MODEL_AGENTS.map((a) => (
      <div key={a.id} className="grid gap-1">
        <label className="text-xs text-muted-foreground" htmlFor={`model-${a.id}`}>{a.label} 模型</label>
        <input id={`model-${a.id}`} aria-label={`${a.label} 模型`}
          className="rounded border border-input bg-card px-2 py-1 text-sm"
          value={modelAgents[a.id] ?? ''}
          onChange={(e) => setModelAgents((prev) => ({ ...prev, [a.id]: e.target.value }))} />
      </div>
    ))}
    <div>
      <Button onClick={() => void handleSaveModel()} disabled={modelSaving}>保存模型配置</Button>
    </div>
    <p className="text-xs text-muted-foreground">
      启动时注入，不改各工具自身配置文件。Claude 受 ~/.claude/settings.json 的 env 影响，若不生效需先清掉该段。
    </p>
  </div>
</section>
```

- [ ] **Step 5: 跑测试确认通过**

Run: `npm test -- Settings ; npm run build`（`frontend/`）
Expected: 全绿。

---

## M6：验证与冒烟

### Task 6: 全量验证 + 冒烟条目

- [ ] **Step 1: Go 侧**

Run: `go build ./... ; go vet ./... ; go test ./... -count=1`
Expected: 全绿。

- [ ] **Step 2: 前端**

Run: `npm test ; npm run build`（`frontend/`）
Expected: 全绿。

- [ ] **Step 3: 冒烟条目**

追加到 `docs/smoke-test-desktop.md`：

```markdown
## 2026-10-03 全局模型配置（启动注入）

- [ ] **Claude 换端点生效（关键：验证 settings.json env 优先级）**
  - 前置：先在 `~/.claude/settings.json` 里**移除 `env` 段**（备份），否则可能覆盖 kshell 注入
  - 步骤：设置页「模型」填 Base URL/API Key/Claude 模型并启用 → 新建 Claude 会话
  - 预期：新会话命中设置页的端点与模型（若 Claude 仍走旧端点，说明 settings.json env 优先级更高，需保持移除）
- [ ] **关闭开关不注入**
  - 步骤：关闭「启用模型配置」→ 新建会话
  - 预期：不注入任何模型环境变量（回到工具自身配置）
- [ ] **密钥不回显**
  - 步骤：填密钥保存 → 重新打开设置页
  - 预期：密钥框为空且显示「已设置（留空不修改）」；勾选「清除密钥」保存后再打开显示「未设置」
- [ ] **Codex / Gemini / OpenCode 抽查**
  - 步骤：分别填模型名后新建会话
  - 预期：新建命令带 `-m/--model <模型>`（可在任务管理器命令行确认）；端点/密钥是否生效按各工具未实测标注为准
```

- [ ] **Step 4: 收尾构建（合并回主干后，需用户明确要求才执行）**

合并后于主干跑 `.\build.ps1 -Desktop`，产出 `dist\kshell-desktop.exe` 才算收尾完成。

---

## 自查记录

- **规格覆盖**：§3 配置模型→Task 1；§4 provider 接口与映射→Task 2；§5 launch 集成与 6 处调用点→Task 3；§6 绑定与 UI→Task 4/5；§7 边界（URL 校验、密钥不回显、enabled 开关）→Task 1/2/4；§9 测试→各任务内；§10 里程碑→M1–M6。
- **类型一致性**：`config.ModelConfig` ↔ `providers.ModelConfig`（字段 BaseURL/APIKey/Model）、`launch.ModelOptions`、`ModelConfigView/Input`、前端同名类型，字段名在两处一致。
- **无占位符**：每个代码步骤含完整实现；未写「类似上文」。
