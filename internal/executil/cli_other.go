//go:build !windows

package executil

import "os/exec"

// wrapBatchFile 非 Windows 没有批处理包装问题。
func wrapBatchFile(*exec.Cmd) {}
