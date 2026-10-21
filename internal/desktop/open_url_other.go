//go:build !windows

package desktop

import (
	"os/exec"
	"runtime"

	"github.com/yangk/kshell/internal/executil"
)

func openExternalURLOS(rawURL string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, rawURL)
	executil.HideWindow(cmd)
	return cmd.Start()
}
