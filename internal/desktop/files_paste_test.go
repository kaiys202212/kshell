package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubClipboardFiles 替换系统剪贴板读取注入点（见 files_paste.go 的 readClipboardFilesFn），
// 返回 (文件绝对路径列表, 是否剪切, err)。
func stubClipboardFiles(t *testing.T, files []string, cut bool, err error) {
	t.Helper()
	prev := readClipboardFilesFn
	readClipboardFilesFn = func() ([]string, bool, error) { return files, cut, err }
	t.Cleanup(func() { readClipboardFilesFn = prev })
}

// makeOutsideFile 在工作区外造一个源文件，返回其绝对路径与内容。
func makeOutsideFile(t *testing.T, dir, name, content string) (string, string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, content
}

func TestUniqueDestName(t *testing.T) {
	root := t.TempDir()
	if got, renamed := uniqueDestName(root, "a.txt"); got != "a.txt" || renamed {
		t.Fatalf("无冲突应原名: %s (renamed=%v)", got, renamed)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, renamed := uniqueDestName(root, "a.txt"); got != "a (1).txt" || !renamed {
		t.Fatalf("冲突应加 (1): %s (renamed=%v)", got, renamed)
	}
	if err := os.WriteFile(filepath.Join(root, "a (1).txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := uniqueDestName(root, "a.txt"); got != "a (2).txt" {
		t.Fatalf("二次冲突应加 (2): %s", got)
	}
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := uniqueDestName(root, "d"); got != "d (1)" {
		t.Fatalf("目录冲突: %s", got)
	}
}

func TestPasteFiles_CopyFromOSClipboard(t *testing.T) {
	env := newFilesEnv(t)
	srcDir := t.TempDir()
	src, content := makeOutsideFile(t, srcDir, "hello.txt", contentSample)
	stubClipboardFiles(t, []string{src}, false, nil)

	sum, err := env.app.PasteFiles(env.root, "pkg", nil, false)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if sum.Copied != 1 || sum.Moved != 0 {
		t.Fatalf("汇总不符: %+v", sum)
	}
	dst := filepath.Join(env.root, "pkg", "hello.txt")
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("粘贴结果缺失: %v", err)
	}
	if string(b) != content {
		t.Fatalf("内容不符: %q", string(b))
	}
	// 源仍在（复制语义）
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("复制后源不应被删除: %v", err)
	}
	// 树缓存已作废：ListFiles 可见
	nodes, err := env.app.ListFiles(env.root, "pkg", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "hello.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("ListFiles 未反映粘贴结果")
	}
}

func TestPasteFiles_CutFromOSClipboard(t *testing.T) {
	env := newFilesEnv(t)
	srcDir := t.TempDir()
	src, _ := makeOutsideFile(t, srcDir, "move.txt", contentSample)
	stubClipboardFiles(t, []string{src}, true, nil)

	sum, err := env.app.PasteFiles(env.root, "", nil, false)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if sum.Moved != 1 || sum.Copied != 0 {
		t.Fatalf("DropEffect 剪切应走移动: %+v", sum)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("剪切后源应被移除")
	}
	if _, err := os.Stat(filepath.Join(env.root, "move.txt")); err != nil {
		t.Fatalf("目标缺失: %v", err)
	}
}

func TestPasteFiles_CopyDirRecursive(t *testing.T) {
	env := newFilesEnv(t)
	srcDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(srcDir, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "inner", "x.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubClipboardFiles(t, []string{srcDir}, false, nil)

	sum, err := env.app.PasteFiles(env.root, "", nil, false)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if sum.Copied != 1 {
		t.Fatalf("目录整体计 1 项: %+v", sum)
	}
	b, err := os.ReadFile(filepath.Join(env.root, filepath.Base(srcDir), "inner", "x.go"))
	if err != nil || string(b) != "package x" {
		t.Fatalf("递归复制不符: %v %q", err, string(b))
	}
}

func TestPasteFiles_ConflictAutoRename(t *testing.T) {
	env := newFilesEnv(t)
	srcDir := t.TempDir()
	src, _ := makeOutsideFile(t, srcDir, "readme.md", "new")
	stubClipboardFiles(t, []string{src}, false, nil)

	sum, err := env.app.PasteFiles(env.root, "", nil, false)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if len(sum.Renamed) != 1 {
		t.Fatalf("应带回改名记录: %+v", sum)
	}
	if !strings.Contains(sum.Renamed[0], "readme (1).md") {
		t.Fatalf("改名记录不符: %v", sum.Renamed)
	}
	b, err := os.ReadFile(filepath.Join(env.root, "readme (1).md"))
	if err != nil || string(b) != "new" {
		t.Fatalf("改名结果不符: %v", err)
	}
}

func TestPasteFiles_TreeInternalMove(t *testing.T) {
	env := newFilesEnv(t)
	// 把 main.go 移进 pkg/
	sum, err := env.app.PasteFiles(env.root, "pkg", []string{"main.go"}, true)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if sum.Moved != 1 {
		t.Fatalf("树内移动汇总不符: %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(env.root, "main.go")); !os.IsNotExist(err) {
		t.Fatal("源应已移除")
	}
	if _, err := os.Stat(filepath.Join(env.root, "pkg", "main.go")); err != nil {
		t.Fatalf("目标缺失: %v", err)
	}
}

func TestPasteFiles_TreeInternalCopy(t *testing.T) {
	env := newFilesEnv(t)
	sum, err := env.app.PasteFiles(env.root, "pkg", []string{"main.go"}, false)
	if err != nil {
		t.Fatalf("PasteFiles: %v", err)
	}
	if sum.Copied != 1 {
		t.Fatalf("树内复制汇总不符: %+v", sum)
	}
	if _, err := os.Stat(filepath.Join(env.root, "main.go")); err != nil {
		t.Fatal("复制后源应保留")
	}
}

func TestPasteFiles_MoveIntoSelfRejected(t *testing.T) {
	env := newFilesEnv(t)
	// 把 pkg 移动到自己内部的 subdir：errMoveIntoSelf
	if err := os.Mkdir(filepath.Join(env.root, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := env.app.PasteFiles(env.root, filepath.Join("pkg", "sub"), []string{"pkg"}, true)
	if !errors.Is(err, errMoveIntoSelf) {
		t.Fatalf("移入自身应报 errMoveIntoSelf，got %v", err)
	}
}

func TestPasteFiles_EmptyClipboard(t *testing.T) {
	env := newFilesEnv(t)
	stubClipboardFiles(t, nil, false, errors.New("err.clipboard.file_list_empty"))
	_, err := env.app.PasteFiles(env.root, "", nil, false)
	if err == nil || !strings.Contains(err.Error(), "err.clipboard.file_list_empty") {
		t.Fatalf("空剪贴板错误应向上传播，got %v", err)
	}
}

func TestPasteFiles_TargetValidation(t *testing.T) {
	env := newFilesEnv(t)
	// dirRel 逃逸
	if _, err := env.app.PasteFiles(env.root, "../esc", []string{"main.go"}, true); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("逃逸应报 errPathOutsideWorkspace，got %v", err)
	}
	// 目标不是目录
	if _, err := env.app.PasteFiles(env.root, "main.go", []string{"readme.md"}, true); !errors.Is(err, errNotDir) {
		t.Fatalf("目标非目录应报 errNotDir，got %v", err)
	}
}

func TestPasteFiles_SourceOutsideRejected(t *testing.T) {
	env := newFilesEnv(t)
	_, err := env.app.PasteFiles(env.root, "", []string{"../outside"}, true)
	if !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("树内源逃逸应拒绝，got %v", err)
	}
}

const contentSample = "kshell paste sample\n"
