//go:build windows

package executil

import (
	"os/exec"
	"strconv"
)

// KillProcessTree 用 taskkill /F /T 连带结束 cmd 的全部后代进程，再兜底杀直属
// 进程。npm .cmd / cmd /c / npx 等包装场景下真正干活的进程（node 等）是孙代，
// 只杀直属进程会留下孤儿（historical case：codebuddy --acp 探测堆积 88 个
// ~140MB 的 node 孤儿）。目标进程已退出时 taskkill 失败属正常，错误忽略。
func KillProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	tk := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	HideWindow(tk) // 收尸不闪黑窗
	_ = tk.Run()
	_ = cmd.Process.Kill()
}
