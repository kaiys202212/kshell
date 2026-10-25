//go:build windows

package executil

import "os/exec"

// PrepareProcessGroup Windows 无需进程组：KillProcessTree 走 taskkill /F /T
// 连带整棵树，保持 no-op 与非 Windows API 对齐。
func PrepareProcessGroup(*exec.Cmd) {}
