//go:build !windows

package executil

import "os/exec"

// DetachFromParent 非 Windows 无 Job 对象，保持空操作。
func DetachFromParent(cmd *exec.Cmd) {}
