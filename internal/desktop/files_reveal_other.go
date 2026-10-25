//go:build !windows && !darwin

package desktop

import (
	"os/exec"

	"github.com/yangk/kshell/internal/executil"
)

// revealInOS Linux 等其它平台走 xdg-open；darwin 的选中行为见 files_reveal_darwin.go。
func revealInOS(abs string, isDir bool) error {
	cmd := exec.Command("xdg-open", abs)
	executil.HideWindow(cmd)
	return cmd.Start()
}
