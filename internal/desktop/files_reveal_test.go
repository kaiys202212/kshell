package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRevealInExplorerRejectsOutside(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.RevealInExplorer(env.root, filepath.Join(env.root, "..", "x")); err == nil {
		t.Fatal("越界应失败")
	}
}

func TestRevealInExplorerRejectsMissing(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.RevealInExplorer(env.root, filepath.Join(env.root, "no-such")); err == nil {
		t.Fatal("不存在应失败")
	}
}

// TestRevealInExplorerRejectsOutsideLink：词法在工作区内、解析后越界的 link/junction 应拒绝。
func TestRevealInExplorerRejectsOutsideLink(t *testing.T) {
	env := newFilesEnv(t)
	outside := t.TempDir()
	link := filepath.Join(env.root, "escape")
	if runtime.GOOS == "windows" {
		// 目录 junction 无需管理员（mklink /J）
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err != nil {
			t.Skipf("创建 junction 失败（跳过）: %v\n%s", err, out)
		}
	} else {
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("创建 symlink 失败（跳过）: %v", err)
		}
	}
	if err := env.app.RevealInExplorer(env.root, link); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("越界 link 应报 errPathOutsideWorkspace，got %v", err)
	}
}
