package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
)

// TestBindingsRejectOutsideWorkspace 覆盖深度防御缺口：parseWSRef 的本地分支
// 直接 Clean 返回，不校验工作区归属——绑定方法（含 DeleteEntry=RemoveAll、
// SaveFile 任意写、ReadFileBytes 任意读）可对任意本地路径操作。
// 前端被攻破时这是最后一道防线；与 files_asset 的 underLocalWorkspaceRoot 对齐。
func TestBindingsRejectOutsideWorkspace(t *testing.T) {
	env := newFilesEnv(t)
	// 注册唯一工作区根，outside 在其外
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}
	outside := filepath.Join(filepath.Dir(env.root), "outside-file.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	// 所有绑定方法对工作区外路径一律 errPathOutsideWorkspace
	if _, err := env.app.ListFiles(outside, "", false); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("ListFiles: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.PreviewFile(outside, ""); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("PreviewFile: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.ReadFileForEdit(outside, ""); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("ReadFileForEdit: want errPathOutsideWorkspace, got %v", err)
	}
	if err := env.app.SaveFile(outside, "x.txt", "pwn", "lf"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("SaveFile: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.ReadFileBytes(outside, "x.txt"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("ReadFileBytes: want errPathOutsideWorkspace, got %v", err)
	}
	if err := env.app.DeleteEntry(outside, "x.txt"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("DeleteEntry: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.RenameEntry(outside, "x.txt", "y.txt"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("RenameEntry: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.MoveEntry(outside, "x.txt", ""); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("MoveEntry: want errPathOutsideWorkspace, got %v", err)
	}
	if _, err := env.app.SearchFiles(outside, "x", false); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("SearchFiles: want errPathOutsideWorkspace, got %v", err)
	}
	// 工作区外的文件必须原封不动
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "secret" {
		t.Fatalf("outside file must be untouched, got %q err=%v", data, err)
	}
}

// TestBindingsAllowInsideWorkspace 保证白名单不误伤：注册过的根内的常规操作照常。
func TestBindingsAllowInsideWorkspace(t *testing.T) {
	env := newFilesEnv(t)
	env.app.rawWorkspaces = []discovery.Workspace{{Path: env.root, Kind: discovery.KindLocal}}

	if _, err := env.app.ListFiles(env.root, "", false); err != nil {
		t.Fatalf("ListFiles 根内应放行: %v", err)
	}
	if _, err := env.app.PreviewFile(env.root, filepath.Join(env.root, "readme.md")); err != nil {
		t.Fatalf("PreviewFile 根内应放行: %v", err)
	}
	if err := env.app.SaveFile(env.root, filepath.Join(env.root, "readme.md"), "# demo2", "lf"); err != nil {
		t.Fatalf("SaveFile 根内应放行: %v", err)
	}
}
