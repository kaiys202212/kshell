//go:build !windows && !darwin

package desktop

import (
	"os/exec"
	"path/filepath"

	"github.com/yangk/kshell/internal/executil"
)

// revealInOS Linux 等其它平台走 xdg-open：无「定位选中」语义，文件维持打开
// 所在目录的既有行为；darwin 的选中行为见 files_reveal_darwin.go。
func revealInOS(abs string, isDir bool) error {
	target := abs
	if !isDir {
		target = filepath.Dir(abs)
	}
	cmd := exec.Command("xdg-open", target)
	executil.HideWindow(cmd)
	return cmd.Start()
}
