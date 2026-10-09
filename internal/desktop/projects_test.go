package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
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

// newSSHProjectsEnv 在 newProjectsEnv 基础上挂上连接表与可注入的远端 Runner。
func newSSHProjectsEnv(t *testing.T, run remotefs.Runner) (*App, *discovery.ProjectStore, *remote.Store, *[]string) {
	t.Helper()
	app, store, events := newProjectsEnv(t, nil)
	connStore := remote.NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	if _, err := connStore.Add(remote.Connection{
		ID: "c1", Name: "测试机", Host: "10.0.0.8", User: "root", Port: 22,
	}); err != nil {
		t.Fatalf("添加连接: %v", err)
	}
	app.opts.Store = connStore
	app.opts.RemoteRun = run
	return app, store, connStore, events
}

func TestAddSSHProjectAddsEmitsAndPresentsRef(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "test -d") {
			return []byte("ok\n"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	app, store, _, events := newSSHProjectsEnv(t, run)

	ref, err := app.AddSSHProject("c1", "/home/u/proj")
	if err != nil {
		t.Fatalf("AddSSHProject: %v", err)
	}
	wantRef := discovery.FormatSSHRef("c1", "/home/u/proj")
	if ref != wantRef {
		t.Fatalf("ref = %q, want %q", ref, wantRef)
	}
	entries := store.ManualEntries()
	if len(entries) != 1 || entries[0].Kind != discovery.KindSSH || entries[0].ConnID != "c1" || entries[0].Path != "/home/u/proj" {
		t.Fatalf("项目表: %+v", entries)
	}
	if !containsEvent(*events, "projects:changed") {
		t.Fatalf("应广播 projects:changed: %v", *events)
	}
	list := app.GetWorkspaces()
	if len(list) != 1 {
		t.Fatalf("工作区数: %+v", list)
	}
	w := list[0]
	if w.Path != wantRef || w.RemotePath != "/home/u/proj" || w.Kind != discovery.KindSSH || w.ConnID != "c1" || w.ConnName != "测试机" {
		t.Fatalf("呈现字段不符: %+v", w)
	}
}

func TestAddSSHProjectRejectsUnknownConn(t *testing.T) {
	app, store, _, _ := newSSHProjectsEnv(t, nil)
	if _, err := app.AddSSHProject("missing", "/home/u"); err == nil {
		t.Fatal("未知连接应报错")
	}
	if len(store.ManualEntries()) != 0 {
		t.Fatalf("不应写盘: %+v", store.ManualEntries())
	}
}

func TestAddSSHProjectRejectsEmptyPath(t *testing.T) {
	app, store, _, _ := newSSHProjectsEnv(t, nil)
	if _, err := app.AddSSHProject("c1", "  "); err == nil {
		t.Fatal("空路径应报错")
	}
	if len(store.ManualEntries()) != 0 {
		t.Fatalf("不应写盘: %+v", store.ManualEntries())
	}
}

func TestAddSSHProjectRejectsNonDirectory(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "test -d") {
			return nil, []byte("not a directory"), fmt.Errorf("exit 1")
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	app, store, _, _ := newSSHProjectsEnv(t, run)
	if _, err := app.AddSSHProject("c1", "/home/u/file.txt"); err == nil {
		t.Fatal("非目录应报错")
	}
	if len(store.ManualEntries()) != 0 {
		t.Fatalf("不应写盘: %+v", store.ManualEntries())
	}
}

func TestListRemoteDirDefaultsToHome(t *testing.T) {
	var cmds []string
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "echo") || strings.Contains(cmd, "$HOME") {
			return []byte("/home/u"), nil, nil
		}
		if strings.Contains(cmd, "find") {
			return []byte("d\t0\t1700000000.0\tproj\n-\t12\t1700000001.0\treadme.md\n"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	app, _, _, _ := newSSHProjectsEnv(t, run)

	got, err := app.ListRemoteDir("c1", "")
	if err != nil {
		t.Fatalf("ListRemoteDir: %v", err)
	}
	if got.Dir != "/home/u" {
		t.Fatalf("Dir=%q, want /home/u", got.Dir)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries=%+v", got.Entries)
	}
	if got.Entries[0].Name != "proj" || !got.Entries[0].IsDir {
		t.Fatalf("首项应为目录 proj: %+v", got.Entries[0])
	}
	if got.Entries[1].Name != "readme.md" || got.Entries[1].IsDir {
		t.Fatalf("次项应为文件: %+v", got.Entries[1])
	}
	if len(cmds) < 2 {
		t.Fatalf("应先解析 HOME 再 ListDir, cmds=%v", cmds)
	}
}

func TestListRemoteDirUnknownConn(t *testing.T) {
	app, _, _, _ := newSSHProjectsEnv(t, nil)
	if _, err := app.ListRemoteDir("nope", "/tmp"); err == nil {
		t.Fatal("未知连接应报错")
	}
}

func TestHideSSHProjectUsesRef(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "test -d") {
			return []byte("ok\n"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	app, _, _, _ := newSSHProjectsEnv(t, run)
	ref, err := app.AddSSHProject("c1", "/home/u/proj")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.HideProject(ref); err != nil {
		t.Fatalf("HideProject: %v", err)
	}
	if len(app.GetWorkspaces()) != 0 {
		t.Fatalf("隐藏后列表应空: %+v", app.GetWorkspaces())
	}
	deleted := app.GetDeletedProjects()
	if len(deleted) != 1 || deleted[0].Path != ref || deleted[0].Name != "proj" || !deleted[0].Exists {
		t.Fatalf("回收站: %+v", deleted)
	}
	if err := app.RestoreProject(ref); err != nil {
		t.Fatalf("RestoreProject: %v", err)
	}
	if len(app.GetWorkspaces()) != 1 {
		t.Fatalf("还原后应有一项: %+v", app.GetWorkspaces())
	}
}
