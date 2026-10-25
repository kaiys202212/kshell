//go:build !windows

package executil

import "os/exec"

// KillProcessTree 非 Windows 平台无进程树语义（子进程随父进程管道关闭/终止
// 传播，各工具自身处理），直接结束直属进程即可。
func KillProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
