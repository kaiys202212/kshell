//go:build !windows

package executil

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
