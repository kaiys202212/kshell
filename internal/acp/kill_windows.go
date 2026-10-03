//go:build windows

package acp

import (
	"os/exec"
	"strconv"

	"github.com/yangk/kshell/internal/executil"
)

// killProcessTree 用 taskkill /T 连带结束后代进程：npx/cmd 包装下真正的 node agent 不会成孤儿。
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	tk := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	executil.HideWindow(tk) // 关闭页签时不要闪一下黑窗
	_ = tk.Run()
	_ = cmd.Process.Kill()
}
