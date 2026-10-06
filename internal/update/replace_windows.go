//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yangk/kshell/internal/executil"
)

func psLiteral(p string) string {
	return strings.ReplaceAll(p, "'", "''")
}

func replaceWaitStartPowerShell(pid int, currentExe, newExe string) string {
	cur := psLiteral(currentExe)
	nxt := psLiteral(newExe)
	// 等旧进程退出并尽量脱离单实例锁；Move-Item 遇文件占用则重试。
	return fmt.Sprintf(`$ErrorActionPreference='Stop'; Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; $ok=$false; for($i=0; $i -lt 40; $i++){ try { Move-Item -LiteralPath '%s' -Destination '%s' -Force; $ok=$true; break } catch { Start-Sleep -Milliseconds 250 } }; if(-not $ok){ throw 'replace failed' }; Start-Sleep -Milliseconds 500; Start-Process -LiteralPath '%s' -WindowStyle Normal`,
		pid, nxt, cur, cur)
}

func spawnReplaceCmd(currentExe, newExe string) *exec.Cmd {
	ps := replaceWaitStartPowerShell(os.Getpid(), currentExe, newExe)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	executil.DetachFromParent(cmd)
	return cmd
}

func spawnReplaceDefault(currentExe, newExe string) error {
	return spawnReplaceCmd(currentExe, newExe).Start()
}
