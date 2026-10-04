//go:build windows

package desktop

import (
	"os/exec"

	"github.com/yangk/kshell/internal/executil"
)

func revealInOS(abs string, isDir bool) error {
	cmd := exec.Command("explorer", explorerArgs(abs, isDir)...)
	executil.HideWindow(cmd)
	return cmd.Start()
}

func explorerArgs(abs string, isDir bool) []string {
	if isDir {
		return []string{abs}
	}
	return []string{"/select," + abs}
}
