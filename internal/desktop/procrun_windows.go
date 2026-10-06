//go:build windows

package desktop

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

// powershellProcScanCmd 构造查询 dir 下运行进程的 PowerShell 命令。
// 单引号翻倍转义；用 OrdinalIgnoreCase.StartsWith 而非 -like，规避通配符注入。
func powershellProcScanCmd(dir string) string {
	escaped := strings.ReplaceAll(dir, "'", "''")
	return "Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith('" + escaped +
		"', [StringComparison]::OrdinalIgnoreCase) } | Select-Object -First 1 -ExpandProperty Path"
}

// runningProcessUnderDir 枚举运行中进程的可执行路径，判断是否位于 dir 之下。
// 只在工具已判定残缺时才会被调用（低频），PowerShell 启动开销可接受。
func runningProcessUnderDir(dir string) bool {
	if dir == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", powershellProcScanCmd(dir))
	executil.HideWindow(cmd)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}
