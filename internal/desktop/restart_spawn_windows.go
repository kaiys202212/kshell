//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yangk/kshell/internal/executil"
)

func restartWaitStartPowerShell(pid int, exe string) string {
	lit := strings.ReplaceAll(exe, "'", "''")
	return fmt.Sprintf(
		"Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; Start-Process -LiteralPath '%s' -ArgumentList '--after-update' -WindowStyle Normal",
		pid, lit,
	)
}

func spawnSelfDelayedCmd(exe string) *exec.Cmd {
	ps := restartWaitStartPowerShell(os.Getpid(), exe)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	executil.DetachFromParent(cmd)
	return cmd
}

func spawnSelfDelayedImpl(exe string) error {
	return spawnSelfDelayedCmd(exe).Start()
}
