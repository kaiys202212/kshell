package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/config"
)

// newSyncApp 构造带隔离 home 的 App：agentsync 的落盘全部进临时目录。
func newSyncApp(t *testing.T) (*App, config.Layout, string) {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	app := NewAppWith(Options{Config: config.Default(), Layout: layout, Home: home})
	return app, layout, home
}

func setSyncPlatform(t *testing.T, app *App) {
	t.Helper()
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "P1",
			OpenAIBaseURL: "https://api.example.com",
			Models:        []string{"m1", "m2"},
		}},
	}
	app.mu.Unlock()
}

func opencodeJSON(home string) (string, error) {
	b, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	return string(b), err
}

func TestToolSyncPlatformRoundTrip(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	if err := app.SetToolSyncConfig("claude", ToolSyncInput{
		Enabled:        true,
		Claude:         ClaudeSlotsView{Model: "m2"},
		ClaudePlatform: "p1",
	}); err != nil {
		t.Fatalf("SetToolSyncConfig: %v", err)
	}
	v, err := app.GetToolSyncConfig("claude")
	if err != nil {
		t.Fatalf("GetToolSyncConfig: %v", err)
	}
	if v.ClaudePlatform != "p1" {
		t.Fatalf("ClaudePlatform = %q, want p1", v.ClaudePlatform)
	}
}

func TestSetToolSyncConfigDropsUnknownPlatform(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	if err := app.SetToolSyncConfig("claude", ToolSyncInput{
		Enabled:        true,
		Claude:         ClaudeSlotsView{Model: "m1"},
		ClaudePlatform: "ghost",
	}); err != nil {
		t.Fatalf("SetToolSyncConfig: %v", err)
	}
	v, err := app.GetToolSyncConfig("claude")
	if err != nil {
		t.Fatalf("GetToolSyncConfig: %v", err)
	}
	if v.ClaudePlatform != "" {
		t.Fatalf("悬空平台应被丢弃, got %q", v.ClaudePlatform)
	}
}

func TestDeletePlatformClearsToolPlatforms(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.ToolSync.ToolPlatforms = map[string]string{"claude": "p1", "codebuddy": "p1"}
	app.mu.Unlock()

	if err := app.DeleteModelPlatform("p1"); err != nil {
		t.Fatalf("DeleteModelPlatform: %v", err)
	}
	m := app.snapshot().Config.Model
	if len(m.ToolSync.ToolPlatforms) != 0 {
		t.Fatalf("删除平台应清空对应工具平台映射, got %#v", m.ToolSync.ToolPlatforms)
	}
	if m.ToolSync.ClaudePlatform != "" || m.ToolSync.CodeBuddyPlatform != "" {
		t.Fatalf("旧字段不应复活: %q/%q", m.ToolSync.ClaudePlatform, m.ToolSync.CodeBuddyPlatform)
	}
}

// 真实 SetToolSyncConfig → saveConfig 路径：映射必须保留（更新一个工具不能抹掉
// 其它工具），落盘只出现 tool_platforms，旧 claude_platform/codebuddy_platform 不回写。
func TestSetToolSyncConfigPersistsToolPlatformMap(t *testing.T) {
	app, layout, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	// 预置新格式映射，并用旧字段模拟未归一化的内存配置（应被迁入映射、随后清空）。
	app.mu.Lock()
	app.opts.Config.Model.ToolSync.ToolPlatforms = map[string]string{"codebuddy": "p1"}
	app.opts.Config.Model.ToolSync.ClaudePlatform = "p1"
	app.mu.Unlock()

	if err := app.SetToolSyncConfig("claude", ToolSyncInput{
		Enabled:        true,
		Claude:         ClaudeSlotsView{Model: "m1"},
		ClaudePlatform: "p1",
	}); err != nil {
		t.Fatalf("SetToolSyncConfig(claude): %v", err)
	}
	// 再更新另一个工具：不能抹掉 claude/codebuddy 的映射。
	if err := app.SetToolSyncConfig("opencode", ToolSyncInput{
		Enabled:  true,
		OpenCode: map[string][]string{"p1": {"m1"}},
	}); err != nil {
		t.Fatalf("SetToolSyncConfig(opencode): %v", err)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tp := loaded.Model.ToolSync.ToolPlatforms
	if tp["claude"] != "p1" || tp["codebuddy"] != "p1" {
		t.Fatalf("ToolPlatforms = %#v, want claude/codebuddy 均为 p1", tp)
	}
	raw, err := os.ReadFile(layout.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "claude_platform") || strings.Contains(string(raw), "codebuddy_platform") {
		t.Fatalf("保存后不应回写旧字段:\n%s", raw)
	}
	if !strings.Contains(string(raw), "tool_platforms") {
		t.Fatalf("保存后应写 tool_platforms:\n%s", raw)
	}
}

// 启用 + 勾选：立即按勾选注入，未勾选模型不出现；配置持久化。
func TestSetToolSyncConfigEnableInjectsSelection(t *testing.T) {
	app, layout, home := newSyncApp(t)
	setSyncPlatform(t, app)

	err := app.SetToolSyncConfig("opencode", ToolSyncInput{
		Enabled:  true,
		OpenCode: map[string][]string{"p1": {"m1"}},
	})
	if err != nil {
		t.Fatalf("SetToolSyncConfig: %v", err)
	}
	raw, err := opencodeJSON(home)
	if err != nil || !strings.Contains(raw, "kshell-p1") {
		t.Fatalf("应注入 kshell-p1: %q err=%v", raw, err)
	}
	if strings.Contains(raw, `"m2"`) {
		t.Fatalf("未勾选的 m2 不应注入: %s", raw)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Model.ToolSync.Enabled["opencode"] {
		t.Fatalf("ToolSync.Enabled = %#v", loaded.Model.ToolSync.Enabled)
	}
	if got := loaded.Model.ToolSync.OpenCode["p1"]; len(got) != 1 || got[0] != "m1" {
		t.Fatalf("OpenCode = %#v", loaded.Model.ToolSync.OpenCode)
	}
}

// 禁用：勾选一并清空，已注入的 kshell 配置立即清除；非 kshell 内容不受影响。
func TestSetToolSyncConfigDisableCleansInjections(t *testing.T) {
	app, layout, home := newSyncApp(t)
	setSyncPlatform(t, app)
	in := agentsync.InputFromModel(func() config.ModelConfig {
		app.mu.Lock()
		defer app.mu.Unlock()
		mc := app.opts.Config.Model
		mc.ToolSync = config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true},
			OpenCode: map[string][]string{"p1": {"m1"}},
		}
		return mc
	}())
	if err := agentsync.SyncTool(home, "opencode", in); err != nil {
		t.Fatalf("预置注入失败: %v", err)
	}

	if err := app.SetToolSyncConfig("opencode", ToolSyncInput{Enabled: false}); err != nil {
		t.Fatalf("SetToolSyncConfig(off): %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.ToolSync.Enabled["opencode"] {
		t.Fatal("禁用后 Enabled 不应含 opencode")
	}
	if len(loaded.Model.ToolSync.OpenCode) != 0 {
		t.Fatalf("禁用应清空勾选, got %#v", loaded.Model.ToolSync.OpenCode)
	}
	raw, err := opencodeJSON(home)
	if err != nil {
		t.Fatalf("opencode.json 应存在: %v", err)
	}
	if strings.Contains(raw, "kshell-p1") {
		t.Fatalf("关闭后 kshell 注入物应被清除, got %s", raw)
	}
}

// claude 槽位：启用后按 4 槽写入 settings.json env；禁用后模型键清掉。
func TestSetToolSyncConfigClaudeSlots(t *testing.T) {
	app, _, home := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.Platforms[0].AnthropicBaseURL = "https://ant.example/"
	app.mu.Unlock()

	err := app.SetToolSyncConfig("claude", ToolSyncInput{
		Enabled: true,
		Claude:  ClaudeSlotsView{Model: "m1", Opus: "m2"},
	})
	if err != nil {
		t.Fatalf("SetToolSyncConfig: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if !strings.Contains(raw, `"ANTHROPIC_MODEL": "m1"`) || !strings.Contains(raw, `"ANTHROPIC_DEFAULT_OPUS_MODEL": "m2"`) {
		t.Fatalf("槽位应写入 env: %s", raw)
	}
	if strings.Contains(raw, "ANTHROPIC_DEFAULT_SONNET_MODEL") {
		t.Fatalf("空槽不应写键: %s", raw)
	}

	if err := app.SetToolSyncConfig("claude", ToolSyncInput{Enabled: false}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	b, err = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ANTHROPIC_MODEL") {
		t.Fatalf("禁用后模型键应清除: %s", b)
	}
}

// 非平台模型同步目标的工具 ID 拒绝设置：配置保持不变。
func TestSetToolSyncConfigRejectsUnknownTool(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)

	if err := app.SetToolSyncConfig("codex", ToolSyncInput{Enabled: true}); err == nil {
		t.Fatal("codex 不是同步目标，应报错")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.opts.Config.Model.ToolSync.Enabled) != 0 {
		t.Fatalf("配置不应被改动, got %#v", app.opts.Config.Model.ToolSync)
	}
}

// Save 失败时内存配置不得被改坏：mutate 里的 map/slice 复用若写穿共享底层数组，
// 失败路径会静默损坏内存配置。
func TestSetToolSyncConfigSaveFailureLeavesMemoryIntact(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.ToolSync = config.ToolSyncConfig{
		Enabled:   map[string]bool{"opencode": true, "claude": true},
		OpenCode:  map[string][]string{"p1": {"m1"}},
		CodeBuddy: []string{"m1"},
	}
	// Config 路径指向目录 → config.Save 必失败
	app.opts.Layout.Config = t.TempDir()
	app.mu.Unlock()

	if err := app.SetToolSyncConfig("codebuddy", ToolSyncInput{Enabled: true, CodeBuddy: []string{"m2"}}); err == nil {
		t.Fatal("Save 应失败")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	got := app.opts.Config.Model.ToolSync
	if !got.Enabled["opencode"] || !got.Enabled["claude"] || got.Enabled["codebuddy"] {
		t.Fatalf("Enabled 被改坏: %#v", got.Enabled)
	}
	if got.OpenCode["p1"][0] != "m1" || got.CodeBuddy[0] != "m1" {
		t.Fatalf("勾选被改坏: %#v / %#v", got.OpenCode, got.CodeBuddy)
	}
}

// GetToolSyncConfig 返回视图副本，调用方改写不得影响内存配置。
func TestGetToolSyncConfig(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.ToolSync = config.ToolSyncConfig{
		Enabled:   map[string]bool{"claude": true},
		Claude:    config.ClaudeSync{Model: "m1", Sonnet: "m2"},
		OpenCode:  map[string][]string{"p1": {"m1", "m2"}},
		CodeBuddy: []string{"m1"},
	}
	app.mu.Unlock()

	v, err := app.GetToolSyncConfig("claude")
	if err != nil {
		t.Fatalf("GetToolSyncConfig: %v", err)
	}
	if !v.Enabled || v.Claude.Model != "m1" || v.Claude.Sonnet != "m2" {
		t.Fatalf("claude 视图 = %+v", v)
	}
	v.OpenCode["p1"][0] = "tampered"
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.opts.Config.Model.ToolSync.OpenCode["p1"][0] != "m1" {
		t.Fatal("应返回副本而非内部 map")
	}
	if _, err := app.GetToolSyncConfig("codex"); err == nil {
		t.Fatal("codex 不是同步目标，应报错")
	}
}

// SetActiveModelSelection 立即落盘；新平台模型列表不含模型时悬空清空。
func TestSetActiveModelSelectionImmediate(t *testing.T) {
	app, layout, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.Platforms = append(app.opts.Config.Model.Platforms,
		config.ModelPlatform{ID: "p2", Name: "P2", OpenAIBaseURL: "https://p2.example", Models: []string{"m9"}})
	app.opts.Config.Model.ActiveModel = "m1"
	app.mu.Unlock()

	if err := app.SetActiveModelSelection("p2", "m1"); err != nil {
		t.Fatalf("SetActiveModelSelection: %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.ActivePlatformID != "p2" {
		t.Fatalf("ActivePlatformID = %q", loaded.Model.ActivePlatformID)
	}
	if loaded.Model.ActiveModel != "" {
		t.Fatalf("悬空 ActiveModel 应清空, got %q", loaded.Model.ActiveModel)
	}

	// 未知平台回落第一个平台；模型属于 p1 时保留。
	if err := app.SetActiveModelSelection("nope", "m2"); err != nil {
		t.Fatalf("SetActiveModelSelection(nope): %v", err)
	}
	loaded, err = config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.ActivePlatformID != "p1" || loaded.Model.ActiveModel != "m2" {
		t.Fatalf("未知平台应回落第一个并保留合法模型, got %q/%q",
			loaded.Model.ActivePlatformID, loaded.Model.ActiveModel)
	}
}

// SetActiveModelSelection 立即落盘并 TrimSpace。
func TestSetActiveModelSelectionTrimSpace(t *testing.T) {
	app, layout, _ := newSyncApp(t)
	setSyncPlatform(t, app)

	if err := app.SetActiveModelSelection("  p1  ", "  m2  "); err != nil {
		t.Fatalf("SetActiveModelSelection: %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.ActivePlatformID != "p1" || loaded.Model.ActiveModel != "m2" {
		t.Fatalf("selection = %q/%q", loaded.Model.ActivePlatformID, loaded.Model.ActiveModel)
	}
}

// 调度器：防抖窗口内的多次请求合并为一次；执行中的请求记 pending 补跑一次。
func TestModelSyncSchedulerCoalesceAndRerun(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	started := make(chan struct{}, 8)
	s := newModelSyncScheduler(30*time.Millisecond, func() {
		started <- struct{}{}
		mu.Lock()
		runs++
		mu.Unlock()
		time.Sleep(60 * time.Millisecond) // 模拟同步耗时，制造执行中触发
	})

	// 窗口内连发 5 次 → 合并为 1 次。
	for i := 0; i < 5; i++ {
		s.schedule()
		time.Sleep(5 * time.Millisecond)
	}
	<-started
	mu.Lock()
	n := runs
	mu.Unlock()
	if n != 1 {
		t.Fatalf("防抖窗口内 5 次请求应合并为 1 次执行, got %d", n)
	}

	// 执行中再触发 → 跑完补跑一次，之后收敛。
	s.schedule()
	s.schedule()
	// 等待首跑结束 + 补跑完成（补跑不再 sleep，很快）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n = runs
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	n = runs
	mu.Unlock()
	if n != 2 {
		t.Fatalf("执行中触发应补跑一次, got %d 次执行", n)
	}
	time.Sleep(120 * time.Millisecond) // 确认无多余执行
	mu.Lock()
	n = runs
	mu.Unlock()
	if n != 2 {
		t.Fatalf("补跑后不应再执行, got %d 次", n)
	}
}
