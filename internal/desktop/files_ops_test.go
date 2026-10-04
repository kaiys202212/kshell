package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestCreateEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// 根层新建文件
	if _, err := app.CreateEntry(env.root, "", "new.go", false); err != nil {
		t.Fatalf("CreateEntry file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "new.go")); err != nil {
		t.Fatalf("新文件不存在: %v", err)
	}
	// 子目录新建目录
	if _, err := app.CreateEntry(env.root, "pkg", "sub", true); err != nil {
		t.Fatalf("CreateEntry dir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(env.root, "pkg", "sub")); err != nil || !fi.IsDir() {
		t.Fatalf("新目录不存在或类型不对: %v", err)
	}
	// 目标已存在
	if _, err := app.CreateEntry(env.root, "", "main.go", false); !errors.Is(err, errTargetExists) {
		t.Fatalf("重名应报 errTargetExists，got %v", err)
	}
	// 名称非法（含分隔符）
	if _, err := app.CreateEntry(env.root, "", "a/b", false); !errors.Is(err, errInvalidName) {
		t.Fatalf("含分隔符应报 errInvalidName，got %v", err)
	}
	// dirRel 逃逸
	if _, err := app.CreateEntry(env.root, "../esc", "x.go", false); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("目录逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// 树缓存已作废：ListFiles 能看到新文件
	nodes, err := app.ListFiles(env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "new.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListFiles 未反映新建文件（缓存未作废？）")
	}
}

func TestDeleteEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// 删文件
	if err := app.DeleteEntry(env.root, "main.go"); err != nil {
		t.Fatalf("DeleteEntry file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "main.go")); !os.IsNotExist(err) {
		t.Fatal("文件应已删除")
	}
	// 删目录（含子项）
	if err := app.DeleteEntry(env.root, "pkg"); err != nil {
		t.Fatalf("DeleteEntry dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "pkg")); !os.IsNotExist(err) {
		t.Fatal("目录应已删除")
	}
	// 工作区根不可删
	if err := app.DeleteEntry(env.root, ""); !errors.Is(err, errRootUndeletable) {
		t.Fatalf("根目录不可删除应报 errRootUndeletable，got %v", err)
	}
	if err := app.DeleteEntry(env.root, "."); !errors.Is(err, errRootUndeletable) {
		t.Fatalf("根目录不可删除应报 errRootUndeletable，got %v", err)
	}
	// 逃逸拒绝
	if err := app.DeleteEntry(env.root, "../outside"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// ListFiles 反映删除
	nodes, _ := app.ListFiles(env.root, "")
	for _, n := range nodes {
		if n.Name == "readme.md" {
			return
		}
	}
	t.Fatal("readme.md 应仍在树中（未删错）")
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

func TestMoveEntry(t *testing.T) {
	env := newFilesEnv(t)
	app := env.app

	// srcRel 逃逸拒绝
	if _, err := app.MoveEntry(env.root, "../x", "pkg"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("srcRel 逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// 文件移入子目录（保留原名）
	newPath, err := app.MoveEntry(env.root, "main.go", "pkg")
	if err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}
	want := filepath.Join(env.root, "pkg", "main.go")
	if newPath != want {
		t.Fatalf("newPath = %q, want %q", newPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("移动后文件不存在: %v", err)
	}
	// 目标已存在（fixture 的 pkg 下本无 readme.md，先造一个同名文件再撞）
	if _, err := app.CreateEntry(env.root, "pkg", "readme.md", false); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MoveEntry(env.root, "readme.md", "pkg"); !errors.Is(err, errTargetExists) {
		t.Fatalf("目标已存在应报 errTargetExists，got %v", err)
	}
	// 目录移入自身子孙目录拒绝（先重建一个待移目录）
	if err := os.Mkdir(filepath.Join(env.root, "outer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(env.root, "outer", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	// os.Mkdir 绕过了绑定，手动作废缓存让树感知新建目录
	env.app.invalidateTree(env.root)
	if _, err := app.MoveEntry(env.root, "outer", filepath.Join("outer", "inner")); !errors.Is(err, errMoveIntoSelf) {
		t.Fatalf("移入子孙目录应报 errMoveIntoSelf，got %v", err)
	}
	// 移回原位（同目录同名）应 no-op 成功
	if _, err := app.MoveEntry(env.root, "readme.md", ""); err != nil {
		t.Fatalf("原地移动应 no-op: %v", err)
	}
	// 大小写同名移动：Windows 大小写不敏感，视为原地 no-op；
	// Linux 大小写敏感语义不同，不适用该口径
	if runtime.GOOS == "windows" {
		if err := os.Rename(filepath.Join(env.root, "readme.md"), filepath.Join(env.root, "README.MD")); err != nil {
			t.Fatal(err)
		}
		// os.Rename 绕过了绑定，手动作废让树按盘上实际大小写重建
		env.app.invalidateTree(env.root)
		got, err := app.MoveEntry(env.root, "README.MD", "")
		if err != nil {
			t.Fatalf("大小写同名移动应 no-op: %v", err)
		}
		if want := filepath.Join(env.root, "README.MD"); got != want {
			t.Fatalf("no-op 应返回原绝对路径: got %q, want %q", got, want)
		}
		if _, err := os.Stat(filepath.Join(env.root, "README.MD")); err != nil {
			t.Fatalf("no-op 不应有副作用，README.MD 应仍在原位: %v", err)
		}
	}
	// 树缓存已作废
	nodes, _ := app.ListFiles(env.root, "")
	found := false
	for _, n := range nodes {
		if n.Name == "pkg" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListFiles 应含 pkg（缓存作废后重建）")
	}
}

func TestMoveEntryDirectory(t *testing.T) {
	env := newFilesEnv(t)

	// 建 outer/inner 结构（fixture 里本无该目录）
	if err := os.Mkdir(filepath.Join(env.root, "outer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(env.root, "outer", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := env.app.MoveEntry(env.root, "outer", "pkg"); err != nil {
		t.Fatalf("移动目录失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "pkg", "outer", "inner")); err != nil {
		t.Fatalf("pkg/outer/inner 应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "outer")); !os.IsNotExist(err) {
		t.Fatal("原位置应消失")
	}
	// 树缓存已作废：ListFiles 能看到新位置
	nodes, err := env.app.ListFiles(env.root, "pkg")
	if err != nil {
		t.Fatalf("pkg 应可懒加载: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "outer" && n.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListFiles 应含 pkg/outer: %+v", nodes)
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
