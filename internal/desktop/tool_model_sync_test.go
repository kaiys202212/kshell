package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "P1",
			OpenAIBaseURL: "https://api.example.com",
			Models:        []string{"m1"},
		}},
	}
	app.mu.Unlock()
}

func opencodeJSON(home string) (string, error) {
	b, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	return string(b), err
}

// 关闭开关：SyncOffTools 记录工具 ID，且该工具已注入的 kshell 配置立即清除
//（方案 A：清掉恢复原状）；非 kshell 内容与其他目标不受影响。
func TestSetToolModelSyncDisableCleansInjections(t *testing.T) {
	app, layout, home := newSyncApp(t)
	setSyncPlatform(t, app)
	in := agentsync.InputFromModel(func() config.ModelConfig {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.opts.Config.Model
	}())
	if err := agentsync.SyncTool(home, "opencode", in); err != nil {
		t.Fatalf("预置注入失败: %v", err)
	}
	if raw, err := opencodeJSON(home); err != nil || !strings.Contains(raw, "kshell-p1") {
		t.Fatalf("预置注入应存在 kshell-p1: %q err=%v", raw, err)
	}

	if err := app.SetToolModelSync("opencode", false); err != nil {
		t.Fatalf("SetToolModelSync(off): %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Model.SyncOffTools) != 1 || loaded.Model.SyncOffTools[0] != "opencode" {
		t.Fatalf("SyncOffTools = %#v, want [opencode]", loaded.Model.SyncOffTools)
	}
	raw, err := opencodeJSON(home)
	if err != nil {
		t.Fatalf("opencode.json 应存在: %v", err)
	}
	if strings.Contains(raw, "kshell-p1") {
		t.Fatalf("关闭后 kshell 注入物应被清除, got %s", raw)
	}
}

// 重新打开：从 SyncOffTools 移除并立即恢复注入。
func TestSetToolModelSyncEnableReinjects(t *testing.T) {
	app, layout, home := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.SyncOffTools = []string{"opencode"}
	app.mu.Unlock()

	if err := app.SetToolModelSync("opencode", true); err != nil {
		t.Fatalf("SetToolModelSync(on): %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Model.SyncOffTools) != 0 {
		t.Fatalf("SyncOffTools = %#v, want 空", loaded.Model.SyncOffTools)
	}
	raw, err := opencodeJSON(home)
	if err != nil || !strings.Contains(raw, "kshell-p1") {
		t.Fatalf("开启后应恢复注入: %q err=%v", raw, err)
	}
}

// 非平台模型同步目标的工具 ID 拒绝设置：配置保持不变。
func TestSetToolModelSyncRejectsUnknownTool(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.SyncOffTools = []string{"opencode"}
	app.mu.Unlock()

	if err := app.SetToolModelSync("codex", false); err == nil {
		t.Fatal("codex 不是同步目标，应报错")
	}
	// 报错发生在保存之前：内存配置不得被改动。
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.opts.Config.Model.SyncOffTools) != 1 || app.opts.Config.Model.SyncOffTools[0] != "opencode" {
		t.Fatalf("配置不应被改动, got %#v", app.opts.Config.Model.SyncOffTools)
	}
}

// GetModelSyncOffTools 返回当前关闭列表（前端初始状态的唯一来源）。
func TestGetModelSyncOffTools(t *testing.T) {
	app, _, _ := newSyncApp(t)
	app.mu.Lock()
	app.opts.Config.Model.SyncOffTools = []string{"claude", "codebuddy"}
	app.mu.Unlock()

	got := app.GetModelSyncOffTools()
	if len(got) != 2 || got[0] != "claude" || got[1] != "codebuddy" {
		t.Fatalf("GetModelSyncOffTools = %v", got)
	}
	// 返回副本：调用方改写不得影响内存配置。
	got[0] = "tampered"
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.opts.Config.Model.SyncOffTools[0] != "claude" {
		t.Fatal("应返回副本而非内部 slice")
	}
}
