//go:build !windows

package executil

import (
	"os/exec"
	"syscall"
)

// KillProcessTree 非 Windows 平台按进程组终止：仅当 cmd 经 PrepareProcessGroup
// 声明了独立进程组时 kill(-pgid) 连带清理孙进程（npx → 适配器 node 的两层树）；
// 否则维持只杀直属的保守语义——kshell 与子进程同组时盲目 kill(-pid) 会误杀自身。
func KillProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}
