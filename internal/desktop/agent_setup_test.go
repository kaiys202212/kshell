package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func newAgentSetupApp(t *testing.T, dismissed bool, tools []discovery.Tool, ps []providers.Provider) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.AgentSetupDismissed = dismissed
	if ps == nil {
		ps = providers.Builtins()
	}
	app := NewAppWith(Options{
		Config: cfg,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
		Providers: ps,
		Home:      dir,
	})
	setTools(t, app, tools)
	return app
}

func TestNeedsAgentSetupDismissed(t *testing.T) {
	app := newAgentSetupApp(t, true, []discovery.Tool{
		{ID: "gemini", Name: "Gemini CLI", Installed: false},
	}, nil)
	if app.NeedsAgentSetup() {
		t.Fatal("已跳过不应再需要向导")
	}
}

func TestNeedsAgentSetupAllInstalled(t *testing.T) {
	app := newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", Installed: true, BinPath: "claude"},
		{ID: "gemini", Name: "Gemini CLI", Installed: true, BinPath: "gemini"},
	}, nil)
	if app.NeedsAgentSetup() {
		t.Fatal("全部有可执行文件时不应弹出")
	}
}

func TestNeedsAgentSetupMissingInstallable(t *testing.T) {
	app := newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", Installed: true, BinPath: "claude"},
		{ID: "gemini", Name: "Gemini CLI", Installed: false},
	}, nil)
	if !app.NeedsAgentSetup() {
		t.Fatal("有可安装且未装的工具时应弹出")
	}
}

func TestNeedsAgentSetupConfigDirOnlyCountsMissing(t *testing.T) {
	app := newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", Installed: true, Source: "config-dir"},
	}, nil)
	if !app.NeedsAgentSetup() {
		t.Fatal("仅配置目录没有 BinPath 应视为未安装")
	}
}

func TestNeedsAgentSetupGenericWithoutRecipe(t *testing.T) {
	g := providers.Generic{Spec: providers.GenericSpec{ID: "demo", Name: "Demo"}}
	app := newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "demo", Name: "Demo", Installed: false},
	}, []providers.Provider{g})
	if app.NeedsAgentSetup() {
		t.Fatal("无安装配方的自定义工具不应弹出向导")
	}
}

func TestDismissAgentSetupPersists(t *testing.T) {
	app := newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "gemini", Installed: false},
	}, nil)
	if err := app.DismissAgentSetup(); err != nil {
		t.Fatal(err)
	}
	if app.NeedsAgentSetup() {
		t.Fatal("dismiss 后不应再需要向导")
	}
	loaded, err := config.Load(app.snapshot().Layout)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.AgentSetupDismissed {
		t.Fatal("应落盘 agent_setup_dismissed")
	}
}
