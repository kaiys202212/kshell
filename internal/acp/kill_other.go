//go:build !windows

package acp

import "os/exec"

// killProcessTree 非 Windows 平台直接杀子进程（无进程树概念，简单结束即可）。
func killProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
