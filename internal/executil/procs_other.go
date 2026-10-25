//go:build !windows

package executil

import (
	"os/exec"
	"syscall"
)

// PrepareProcessGroup 让子进程自立进程组（组长 pgid==pid），供 KillProcessTree
// 组杀整棵树；不调用则树杀维持「只杀直属」的保守语义，避免误杀同组自身。
func PrepareProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
