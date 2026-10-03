package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yangk/kshell/internal/remote"
)

// newFilesApp 组装绑定层测试环境：临时工作区 + 连接存储打桩。
type filesEnv struct {
	app     *App
	stub    *stubLauncher
	root    string
	binPath string
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()

	root := t.TempDir()
	// 布局：
	//   main.go
	//   readme.md
	//   pkg/inner.go
	//   node_modules/junk.js（内置排除）
	writeFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "readme.md", "# demo\n")
	writeFile(t, root, filepath.Join("pkg", "inner.go"), "package pkg\n")
	writeFile(t, root, filepath.Join("node_modules", "junk.js"), "junk")

	store := remote.NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	if _, err := store.Add(remote.Connection{
		ID: "c1", Name: "测试机", Host: "10.0.0.8", User: "root", Port: 22, Workspace: root,
	}); err != nil {
		t.Fatalf("添加测试连接失败: %v", err)
	}
	if _, err := store.Add(remote.Connection{
		ID: "c2", Name: "全局机", Host: "10.0.0.9", User: "ops", Port: 2222,
	}); err != nil {
		t.Fatalf("添加全局连接失败: %v", err)
	}

	stub := &stubLauncher{}
	app := NewAppWith(Options{
		Home:          root, // 借用临时目录当 home，避免碰真实配置
		ProvidersPath: filepath.Join(t.TempDir(), "providers.yaml"),
		Store:         store,
		Windows:       NewWindowManager(stub, nil),
	})
	return &filesEnv{app: app, stub: stub, root: root}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesListsRootExcludingIgnored(t *testing.T) {
	env := newFilesEnv(t)

	nodes, err := env.app.ListFiles(env.root, "")
	if err != nil {
		t.Fatalf("ListFiles error: %v", err)
	}
	var names []string
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "main.go") || !strings.Contains(joined, "pkg") {
		t.Fatalf("根层缺少预期条目: %v", names)
	}
	if strings.Contains(joined, "node_modules") {
		t.Fatalf("内置排除目录不应出现: %v", names)
	}
	// 排序：目录在前
	if names[0] != "pkg" {
		t.Fatalf("目录应排在文件前: %v", names)
	}
}

func TestListFilesLazyLoadsSubdir(t *testing.T) {
	env := newFilesEnv(t)

	nodes, err := env.app.ListFiles(env.root, "pkg")
	if err != nil {
		t.Fatalf("ListFiles(pkg) error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "inner.go" {
		t.Fatalf("pkg 子项 = %+v", nodes)
	}

	// 二次访问同一目录应命中节点缓存（不再读盘）——通过节点 Loaded 断言
	tree, err := env.app.treeFor(env.root)
	if err != nil {
		t.Fatal(err)
	}
	node, err := env.app.nodeAt(tree, env.root, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if !node.Loaded {
		t.Fatal("访问过的目录节点应标记 Loaded（目录级缓存生效）")
	}
}

func TestListFilesRejectsTraversal(t *testing.T) {
	env := newFilesEnv(t)

	// 词法穿越：.. 与正斜杠写法（前端可能传 / 分隔）都应被拒
	if _, err := env.app.ListFiles(env.root, ".."); err == nil {
		t.Fatal("relPath 越出工作区应报错")
	}
	if _, err := env.app.ListFiles(env.root, "../sibling"); err == nil {
		t.Fatal("正斜杠穿越应报错")
	}
	if _, err := env.app.PreviewFile(env.root, filepath.Join(env.root, "..", "main.go")); err == nil {
		t.Fatal("预览路径越出工作区应报错")
	}
	// 绝对路径输入：Join 后段匹配失败（errDirNotFound），不得越界成功
	if _, err := env.app.ListFiles(env.root, `C:\Windows`); err == nil {
		t.Fatal("绝对路径 relPath 不应返回成功")
	}
}

func TestPreviewFileReturnsContent(t *testing.T) {
	env := newFilesEnv(t)

	p, err := env.app.PreviewFile(env.root, filepath.Join(env.root, "main.go"))
	if err != nil {
		t.Fatalf("PreviewFile error: %v", err)
	}
	if len(p.Lines) == 0 || !strings.Contains(strings.Join(p.Lines, "\n"), "func main()") {
		t.Fatalf("预览内容不符: %+v", p)
	}
	if p.Truncated || p.Binary {
		t.Fatalf("小文本文件不应截断/判二进制: %+v", p)
	}
}

func TestListConnectionsScopesByWorkspace(t *testing.T) {
	env := newFilesEnv(t)

	all := env.app.ListConnections("")
	if len(all) != 2 {
		t.Fatalf("空 wsID 应返回全部连接, got %d", len(all))
	}
	scoped := env.app.ListConnections(env.root)
	if len(scoped) != 2 {
		// c1 绑定工作区 + c2 全局（Workspace 为空），都应命中
		t.Fatalf("工作区连接 + 全局连接应都返回, got %d", len(scoped))
	}
	// 排他性：绑定到其他工作区的连接不得出现（c1 绑定 root，查第三方 wsID 只剩 c2）
	other := env.app.ListConnections(`D:\somewhere-else`)
	if len(other) != 1 || other[0].ID != "c2" {
		t.Fatalf("第三方工作区应只返回全局连接 c2, got %+v", other)
	}
	// All 返回副本：修改返回切片不影响内部状态
	all[0].Host = "篡改"
	if again := env.app.ListConnections(""); again[0].Host == "篡改" {
		t.Fatal("ListConnections 应返回副本")
	}
}

func TestOpenSSHLaunchesWindow(t *testing.T) {
	env := newFilesEnv(t)

	if err := env.app.OpenSSH("c1"); err != nil {
		t.Fatalf("OpenSSH error: %v", err)
	}
	if len(env.stub.launches) != 1 {
		t.Fatalf("launcher.Launch 次数 = %d", len(env.stub.launches))
	}
	call := env.stub.launches[0]
	if call.dir != env.root {
		t.Fatalf("SSH 窗口 Dir = %q, 期望连接绑定的工作区", call.dir)
	}
	if call.title != "kshell · 测试机" {
		t.Fatalf("SSH 窗口标题 = %q", call.title)
	}
	stmt := strings.Join(call.args, " ")
	for _, want := range []string{"ssh", "'-o'", "'BatchMode=yes'", "'-t'", "'root@10.0.0.8'"} {
		if !strings.Contains(stmt, want) {
			t.Fatalf("SSH 语句缺少 %s: %s", want, stmt)
		}
	}

	// 未知连接
	if err := env.app.OpenSSH("nope"); err == nil {
		t.Fatal("未知连接应报错")
	}
}

func TestOpenSSHGlobalConnUsesHomeDir(t *testing.T) {
	env := newFilesEnv(t)

	if err := env.app.OpenSSH("c2"); err != nil {
		t.Fatalf("OpenSSH error: %v", err)
	}
	call := env.stub.launches[0]
	if call.dir != env.root {
		t.Fatalf("全局连接窗口 Dir = %q, 期望用户主目录（测试借用 home）", call.dir)
	}
	if !strings.Contains(strings.Join(call.args, " "), "'-p' '2222'") {
		t.Fatalf("非 22 端口应带 -p 参数: %v", call.args)
	}
}

func TestExecRemoteUnknownConn(t *testing.T) {
	env := newFilesEnv(t)

	// 已知连接路径走真实 ssh（需要可达主机），不在此覆盖；
	// connByID 查找链路已由 OpenSSH 用例验证，这里只锁未知连接的报错契约。
	if _, err := env.app.ExecRemote("nope", "true"); err == nil {
		t.Fatal("未知连接应报错")
	}
}

func TestGetToolsReturnsSnapshot(t *testing.T) {
	app, _, _ := newTestApp(t)
	setTools(t, app, fakeTools)

	got := app.GetTools()
	if len(got) != 1 || got[0].ID != "claude" {
		t.Fatalf("GetTools = %+v", got)
	}
	// 未扫过时返回空切片而非 nil（前端好处理）
	if got := NewApp().GetTools(); got == nil {
		t.Fatal("未扫描时 GetTools 应返回空切片")
	}
}

func TestSaveAndLoadProvidersYAML(t *testing.T) {
	env := newFilesEnv(t)
	content := "providers:\n  - id: demo\n    name: Demo\n"

	if err := env.app.SaveProvidersYAML(content); err != nil {
		t.Fatalf("SaveProvidersYAML error: %v", err)
	}
	got, err := env.app.LoadProvidersYAML()
	if err != nil {
		t.Fatalf("LoadProvidersYAML error: %v", err)
	}
	if got != content {
		t.Fatalf("回读内容不符: %q", got)
	}

	// 非法 YAML 拒绝落盘
	if err := env.app.SaveProvidersYAML("providers: [unclosed"); err == nil {
		t.Fatal("非法 YAML 应报错")
	}
	if got, _ := env.app.LoadProvidersYAML(); got != content {
		t.Fatal("非法 YAML 不应覆盖已有内容")
	}

	// 合法但内容为空也允许保存（清空配置是合法操作）
	if err := env.app.SaveProvidersYAML(""); err != nil {
		t.Fatalf("空内容（合法 YAML）应允许保存: %v", err)
	}
}

func TestLoadProvidersYAMLMissingReturnsTemplate(t *testing.T) {
	env := newFilesEnv(t)

	got, err := env.app.LoadProvidersYAML()
	if err != nil {
		t.Fatalf("LoadProvidersYAML error: %v", err)
	}
	if !strings.Contains(got, "providers:") {
		t.Fatal("文件缺失应回填默认模板")
	}
}

func TestConcurrentListFiles(t *testing.T) {
	env := newFilesEnv(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := env.app.ListFiles(env.root, ""); err != nil {
				t.Errorf("并发 ListFiles 错误: %v", err)
			}
			if _, err := env.app.ListFiles(env.root, "pkg"); err != nil {
				t.Errorf("并发 ListFiles(pkg) 错误: %v", err)
			}
		}()
	}
	wg.Wait()
}
