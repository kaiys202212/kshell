package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// newProjectsEnv 组装项目表测试环境：临时项目文件 + 固定扫描结果 + 事件收集。
// 扫描结果先跑一轮，让 result/rawWorkspaces 落位（applyProjects 只在已有结果时才广播）。
func newProjectsEnv(t *testing.T, workspaces []discovery.Workspace) (*App, *discovery.ProjectStore, *[]string) {
	t.Helper()

	store := discovery.NewProjectStore(filepath.Join(t.TempDir(), "projects.yaml"))
	if err := store.Load(); err != nil {
		t.Fatalf("装载项目表: %v", err)
	}

	var events []string
	app := NewAppWith(Options{
		Home:      t.TempDir(),
		Projects:  store,
		Providers: []providers.Provider{fakeProvider{id: "claude"}},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{Workspaces: append([]discovery.Workspace(nil), workspaces...)}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		Emit:    func(name string, _ ...any) { events = append(events, name) },
	})
	app.runScan() // 让 result/rawWorkspaces 落位
	return app, store, &events
}

func containsEvent(events []string, name string) bool {
	for _, e := range events {
		if e == name {
			return true
		}
	}
	return false
}

func hasWorkspace(list []discovery.Workspace, path string) bool {
	for _, w := range list {
		if w.Path == path {
			return true
		}
	}
	return false
}

// stubPickDirectory 替换原生目录选择器，返回固定结果。
func stubPickDirectory(t *testing.T, dir string, err error) {
	t.Helper()
	orig := pickDirectory
	pickDirectory = func(context.Context, string) (string, error) { return dir, err }
	t.Cleanup(func() { pickDirectory = orig })
}

func TestCreateProjectAddsAndEmits(t *testing.T) {
	dir := t.TempDir()
	app, store, events := newProjectsEnv(t, nil)
	stubPickDirectory(t, dir, nil)

	got, err := app.CreateProject()
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if got != dir {
		t.Fatalf("CreateProject = %q, 期望 %q", got, dir)
	}
	if manual := store.Manual(); len(manual) != 1 || manual[0] != dir {
		t.Fatalf("项目表未落盘: %v", manual)
	}
	if !containsEvent(*events, "projects:changed") {
		t.Fatalf("应广播 projects:changed, got %v", *events)
	}
	if !hasWorkspace(app.GetWorkspaces(), dir) {
		t.Fatalf("新项目未出现在工作区列表: %+v", app.GetWorkspaces())
	}
}

func TestCreateProjectCancel(t *testing.T) {
	app, store, events := newProjectsEnv(t, nil)
	stubPickDirectory(t, "", nil)

	got, err := app.CreateProject()
	if err != nil {
		t.Fatalf("取消不应报错: %v", err)
	}
	if got != "" {
		t.Fatalf("取消应返回空串, got %q", got)
	}
	if manual := store.Manual(); len(manual) != 0 {
		t.Fatalf("取消不应写盘: %v", manual)
	}
	if containsEvent(*events, "projects:changed") {
		t.Fatalf("取消不应广播 projects:changed: %v", *events)
	}
}

func TestCreateProjectRejectsNonDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, store, _ := newProjectsEnv(t, nil)
	stubPickDirectory(t, file, nil)

	if _, err := app.CreateProject(); err == nil {
		t.Fatal("选中文件应报「目录不存在」")
	}
	if manual := store.Manual(); len(manual) != 0 {
		t.Fatalf("非法路径不应写盘: %v", manual)
	}
}

func TestHideAndRestoreProject(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Base(dir)
	app, _, events := newProjectsEnv(t, []discovery.Workspace{{Path: dir, Name: name, Source: "git"}})

	if len(app.GetWorkspaces()) != 1 {
		t.Fatalf("初始应有 1 个工作区: %+v", app.GetWorkspaces())
	}

	if err := app.HideProject(dir); err != nil {
		t.Fatalf("HideProject error: %v", err)
	}
	if len(app.GetWorkspaces()) != 0 {
		t.Fatalf("删除后不应再出现: %+v", app.GetWorkspaces())
	}
	deleted := app.GetDeletedProjects()
	if len(deleted) != 1 {
		t.Fatalf("回收站应有 1 条: %+v", deleted)
	}
	if deleted[0].Path != dir || deleted[0].Name != name || !deleted[0].Exists || deleted[0].At == "" {
		t.Fatalf("回收站条目字段不符: %+v", deleted[0])
	}
	if !containsEvent(*events, "projects:changed") {
		t.Fatalf("删除应广播 projects:changed: %v", *events)
	}

	if err := app.RestoreProject(dir); err != nil {
		t.Fatalf("RestoreProject error: %v", err)
	}
	if len(app.GetWorkspaces()) != 1 || !hasWorkspace(app.GetWorkspaces(), dir) {
		t.Fatalf("还原后应回到列表: %+v", app.GetWorkspaces())
	}
	if len(app.GetDeletedProjects()) != 0 {
		t.Fatalf("还原后回收站应为空: %+v", app.GetDeletedProjects())
	}
}

func TestScanPayloadExcludesMissingWorkspaceDirs(t *testing.T) {
	existsDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "已被物理删除")

	app, _, _ := newProjectsEnv(t, []discovery.Workspace{
		{Path: existsDir, Name: filepath.Base(existsDir), Source: "sessions"},
		{Path: missing, Name: "已被物理删除", Source: "sessions"},
	})

	got := app.GetWorkspaces()
	if len(got) != 1 || got[0].Path != existsDir {
		t.Fatalf("目录不存在的项应被剔除, got %+v", got)
	}
	if app.rawWorkspaces == nil || len(app.rawWorkspaces) != 2 {
		t.Fatalf("原始扫描结果应保留两条（删除/还原靠它派生）: %+v", app.rawWorkspaces)
	}
}

func TestGetDeletedProjectsWithoutStore(t *testing.T) {
	app := NewAppWith(Options{})
	if got := app.GetDeletedProjects(); got == nil || len(got) != 0 {
		t.Fatalf("未装配项目表应返回空切片, got %+v", got)
	}
}
