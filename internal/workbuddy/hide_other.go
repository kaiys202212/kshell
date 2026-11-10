//go:build !windows

package workbuddy

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
