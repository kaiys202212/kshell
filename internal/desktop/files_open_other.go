//go:build !windows

package desktop

import (
	"os/exec"
	"runtime"

	"github.com/yangk/kshell/internal/executil"
)

func openLocalFileOS(abs string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, abs)
	executil.HideWindow(cmd)
	return cmd.Start()
}
