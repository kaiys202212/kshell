package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshFilesSeesExternalCreate(t *testing.T) {
	env := newFilesEnv(t)
	if _, err := env.app.ListFiles(env.root, "", false); err != nil {
		t.Fatal(err)
	}
	// 外部写入（不经 CreateEntry，模拟资源管理器新建）
	writeFile(t, env.root, "ext.go", "package x\n")
	// 缓存未作废前：依赖 Loaded 缓存时应看不到；若已可见则软跳过（缓存语义变化时不挡主断言）
	before, _ := env.app.ListFiles(env.root, "", false)
	sawBefore := false
	for _, n := range before {
		if n.Name == "ext.go" {
			sawBefore = true
			break
		}
	}
	if sawBefore {
		t.Log("作废前已可见（缓存未挡住外部写入）；继续验证 Refresh 后可见")
	}
	if err := env.app.RefreshFiles(env.root); err != nil {
		t.Fatal(err)
	}
	after, err := env.app.ListFiles(env.root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range after {
		if n.Name == "ext.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("RefreshFiles 后应看到 ext.go")
	}
}

// 已加载的子目录：ListFiles 直接命中 Go 树缓存，看不到外部新增文件；
// 前端「展开时主动刷新」依赖先调 RefreshFiles 作废缓存再重拉（FileTree.handleDirToggle），
// 本用例把这条契约钉住——若哪天缓存语义变化导致不作废也能读到新文件，这里会先失败提醒。
func TestRefreshFilesSeesExternalCreateInLoadedSubdir(t *testing.T) {
	env := newFilesEnv(t)
	if _, err := env.app.ListFiles(env.root, "pkg", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, env.root, filepath.Join("pkg", "ext.go"), "package pkg\n")

	before, err := env.app.ListFiles(env.root, "pkg", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range before {
		if n.Name == "ext.go" {
			t.Fatal("缓存未作废时不应看到 pkg/ext.go（前端展开刷新须先 RefreshFiles）")
		}
	}

	if err := env.app.RefreshFiles(env.root); err != nil {
		t.Fatal(err)
	}
	after, err := env.app.ListFiles(env.root, "pkg", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after {
		if n.Name == "ext.go" {
			return
		}
	}
	t.Fatal("RefreshFiles 后应看到 pkg/ext.go")
}

func TestListFilesShowAllRevealsIgnored(t *testing.T) {
	env := newFilesEnv(t)
	// newFilesEnv 已有 node_modules；再写 .gitignore 条目与 .git 目录
	writeFile(t, env.root, ".gitignore", "*.tmp\n")
	writeFile(t, env.root, "x.tmp", "x")
	if err := os.Mkdir(filepath.Join(env.root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	hidden, err := env.app.ListFiles(env.root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range hidden {
		if n.Name == "x.tmp" || n.Name == "node_modules" {
			t.Fatalf("showAll=false 不应出现 %s", n.Name)
		}
	}
	shown, err := env.app.ListFiles(env.root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range shown {
		names = append(names, n.Name)
		if n.Name == ".git" {
			t.Fatal(".git 即使 showAll 也不应出现")
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "x.tmp") || !strings.Contains(joined, "node_modules") {
		t.Fatalf("showAll=true 应含 x.tmp 与 node_modules: %v", names)
	}
}
