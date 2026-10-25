//go:build darwin

package desktop

import (
	"os/exec"

	"github.com/yangk/kshell/internal/executil"
)

// revealInOS darwin 实现：文件用 open -R 在 Finder 中选中目标（对齐 Windows
// explorer /select, 的行为），目录直接打开。
func revealInOS(abs string, isDir bool) error {
	cmd := exec.Command("open", revealArgs(abs, isDir)...)
	executil.HideWindow(cmd)
	return cmd.Start()
}

func revealArgs(abs string, isDir bool) []string {
	if isDir {
		return []string{abs}
	}
	return []string{"-R", abs}
}
