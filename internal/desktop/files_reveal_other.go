//go:build !windows

package desktop

import (
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/yangk/kshell/internal/executil"
)

func revealInOS(abs string, isDir bool) error {
	target := abs
	if !isDir {
		target = filepath.Dir(abs)
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, target)
	executil.HideWindow(cmd)
	return cmd.Start()
}
