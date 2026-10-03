package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/workspace"
)

func TestSearchFilesBinding(t *testing.T) {
	env := newFilesEnv(t)

	hits, err := env.app.SearchFiles(env.root, "main")
	if err != nil {
		t.Fatalf("SearchFiles error: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "main.go" || hits[0].IsDir {
		t.Fatalf("命中不符: %+v", hits)
	}
	if hits[0].RelPath != "main.go" {
		t.Fatalf("RelPath = %q", hits[0].RelPath)
	}
	// node_modules 内的文件被内置排除
	if hits2, _ := env.app.SearchFiles(env.root, "junk"); len(hits2) != 0 {
		t.Fatalf("内置排除目录不应命中: %+v", hits2)
	}
	if hits3, _ := env.app.SearchFiles(env.root, "  "); len(hits3) != 0 {
		t.Fatal("空查询应返回空")
	}
	if hits4, _ := env.app.SearchFiles(env.root, "PKG"); len(hits4) != 1 || !hits4[0].IsDir {
		t.Fatalf("目录应大小写不敏感命中: %+v", hits4)
	}
}

func TestRenameEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	newPath, err := app.RenameEntry(env.root, "main.go", "renamed.go")
	if err != nil {
		t.Fatalf("RenameEntry error: %v", err)
	}
	want := filepath.Join(env.root, "renamed.go")
	if newPath != want {
		t.Fatalf("newPath = %q", newPath)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("新文件不存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "main.go")); !os.IsNotExist(err) {
		t.Fatal("旧文件应已消失")
	}

	// 树缓存已作废，ListFiles 反映新名字（且树自动重建）
	nodes, err := app.ListFiles(env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "renamed.go" {
			found = true
		}
		if n.Name == "main.go" {
			t.Fatal("旧名字不应仍在树里")
		}
	}
	if !found {
		t.Fatalf("新名字未进树: %+v", nodes)
	}
}

func TestRenameEntryDirectory(t *testing.T) {
	env := newFilesEnv(t)

	if _, err := env.app.RenameEntry(env.root, "pkg", "lib"); err != nil {
		t.Fatalf("重命名目录失败: %v", err)
	}
	nodes, err := env.app.ListFiles(env.root, "lib")
	if err != nil {
		t.Fatalf("新目录应可懒加载: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "inner.go" {
		t.Fatalf("子文件应跟随目录: %+v", nodes)
	}
}

func TestRenameEntryValidation(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	cases := []struct {
		name    string
		rel     string
		newName string
		wantErr error
	}{
		{"空名", "main.go", "  ", errInvalidName},
		{"点号", "main.go", ".", errInvalidName},
		{"点点", "main.go", "..", errInvalidName},
		{"含分隔符-正斜杠", "main.go", "a/b.go", errInvalidName},
		{"含分隔符-反斜杠", "main.go", `a\b.go`, errInvalidName},
		{"目标已存在", "main.go", "readme.md", errTargetExists},
		{"越出工作区", "../escape.txt", "x.txt", errPathOutsideWorkspace}, // relPath 先被 nodeAt 的 underPath 拒
	}
	for _, c := range cases {
		if _, err := app.RenameEntry(env.root, c.rel, c.newName); !errors.Is(err, c.wantErr) {
			t.Errorf("%s：期望 %v，实得 %v", c.name, c.wantErr, err)
		}
	}

	// 仅大小写变化：Windows 上合法改名
	if got, err := app.RenameEntry(env.root, "readme.md", "README.MD"); err != nil || got == "" {
		t.Fatalf("仅大小写改名应放行: %q %v", got, err)
	}
}

func TestRenameEntryEscapesBlocked(t *testing.T) {
	env := newFilesEnv(t)

	if _, err := env.app.RenameEntry(env.root, "main.go", ".."); err == nil {
		t.Fatal(".. 名字应被拒")
	}
}

func TestReadForEditBinding(t *testing.T) {
	env := newFilesEnv(t)

	ec, err := env.app.ReadFileForEdit(env.root, filepath.Join(env.root, "readme.md"))
	if err != nil {
		t.Fatalf("ReadFileForEdit error: %v", err)
	}
	if ec.Text != "# demo\n" || ec.EOL != "lf" {
		t.Fatalf("内容不符: %+v", ec)
	}

	// 穿越
	if _, err := env.app.ReadFileForEdit(env.root, filepath.Join(env.root, "..", "outside.txt")); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("越界读取应被拒: %v", err)
	}
	// 二进制
	binPath := filepath.Join(env.root, "bin.dat")
	if err := os.WriteFile(binPath, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.app.ReadFileForEdit(env.root, binPath); !errors.Is(err, workspace.ErrBinaryFile) {
		t.Fatalf("二进制应被拒: %v", err)
	}
}

func TestSaveFileBinding(t *testing.T) {
	env := newFilesEnv(t)

	p := filepath.Join(env.root, "readme.md")
	if err := env.app.SaveFile(env.root, p, "# demo\n\nedited\r\nline\n", "lf"); err != nil {
		t.Fatalf("SaveFile error: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// lf 模式下编辑文本统一为 \n（与 ReadForEdit 归一口径一致）
	if string(raw) != "# demo\n\nedited\nline\n" {
		t.Fatalf("lf 模式应归一为 \\n: %q", raw)
	}

	// crlf 还原
	if err := env.app.SaveFile(env.root, p, "a\nb\n", "crlf"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	if string(raw) != "a\r\nb\r\n" {
		t.Fatalf("crlf 应还原: %q", raw)
	}

	// 保存后重新读取，编辑往返无损
	ec, err := env.app.ReadFileForEdit(env.root, p)
	if err != nil {
		t.Fatal(err)
	}
	if ec.Text != "a\nb\n" || ec.EOL != "crlf" {
		t.Fatalf("往返不符: %+v", ec)
	}

	// 穿越
	if err := env.app.SaveFile(env.root, filepath.Join(env.root, "..", "x.txt"), "x", "lf"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("越界保存应被拒: %v", err)
	}
}

func TestGitStatusBinding(t *testing.T) {
	env := newFilesEnv(t)

	res, err := env.app.GitStatus(env.root)
	if err != nil {
		t.Fatalf("GitStatus error: %v", err)
	}
	if res.IsRepo || len(res.Status) != 0 {
		t.Fatalf("临时目录非 git 仓库: isRepo=%v status=%v", res.IsRepo, res.Status)
	}
}
