package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// newProjectSetupApp 组装用于首次项目选择弹窗判断的 App。
// configFileExisted 模拟启动时 ~/.kshell/config.yaml 是否已存在（老用户）。
func newProjectSetupApp(t *testing.T, dismissed, configFileExisted bool, workspaces []discovery.Workspace) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.ProjectSetupDismissed = dismissed
	var res *discovery.Result
	if workspaces != nil {
		res = &discovery.Result{Workspaces: workspaces}
	}
	app := NewAppWith(Options{
		Config:            cfg,
		ConfigFileExisted: configFileExisted,
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
		Providers: providers.Builtins(),
		Home:      dir,
	})
	// 直接落位扫描结果（runScan 链路另有扫描器测试覆盖，这里只测判定函数）
	app.mu.Lock()
	app.result = res
	app.rawWorkspaces = workspaces
	app.mu.Unlock()
	return app
}

func TestNeedsProjectSetupDismissed(t *testing.T) {
	app := newProjectSetupApp(t, true, false, []discovery.Workspace{
		{Path: `D:\ws-a`, Name: "ws-a", Kind: discovery.KindLocal},
	})
	if app.NeedsProjectSetup() {
		t.Fatal("已落盘标记不应再弹")
	}
}

func TestNeedsProjectSetupConfigFileExisted(t *testing.T) {
	app := newProjectSetupApp(t, false, true, []discovery.Workspace{
		{Path: `D:\ws-a`, Name: "ws-a", Kind: discovery.KindLocal},
	})
	if app.NeedsProjectSetup() {
		t.Fatal("老用户（配置文件已存在）不应弹")
	}
}

func TestNeedsProjectSetupNotScanned(t *testing.T) {
	app := newProjectSetupApp(t, false, false, nil)
	if app.NeedsProjectSetup() {
		t.Fatal("未完成首次扫描不应弹")
	}
}

func TestNeedsProjectSetupEmptyWorkspaces(t *testing.T) {
	// 空结果仍返回 true：由前端拉到空列表后落盘标记放弃引导（见 project_setup.go 注释）
	app := newProjectSetupApp(t, false, false, []discovery.Workspace{})
	if !app.NeedsProjectSetup() {
		t.Fatal("扫描完成但列表为空应仍进入引导（前端负责空结果落盘）")
	}
}

func TestNeedsProjectSetupReady(t *testing.T) {
	app := newProjectSetupApp(t, false, false, []discovery.Workspace{
		{Path: `D:\ws-a`, Name: "ws-a", Kind: discovery.KindLocal},
		{Path: `D:\ws-b`, Name: "ws-b", Kind: discovery.KindLocal},
	})
	if !app.NeedsProjectSetup() {
		t.Fatal("新用户且扫出工作区时应弹")
	}
}

func TestDismissProjectSetupPersists(t *testing.T) {
	app := newProjectSetupApp(t, false, false, []discovery.Workspace{
		{Path: `D:\ws-a`, Name: "ws-a", Kind: discovery.KindLocal},
	})
	if err := app.DismissProjectSetup(); err != nil {
		t.Fatal(err)
	}
	if app.NeedsProjectSetup() {
		t.Fatal("dismiss 后不应再需要弹窗")
	}
	loaded, err := config.Load(app.snapshot().Layout)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ProjectSetupDismissed {
		t.Fatal("应落盘 project_setup_dismissed")
	}
}
