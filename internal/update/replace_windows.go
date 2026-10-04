//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func spawnReplaceDefault(currentExe, newExe string) error {
	cur := strings.ReplaceAll(currentExe, "'", "''")
	nxt := strings.ReplaceAll(newExe, "'", "''")
	ps := fmt.Sprintf(
		"Wait-Process -Id %d -ErrorAction SilentlyContinue; Move-Item -LiteralPath '%s' -Destination '%s' -Force; Start-Process -LiteralPath '%s'",
		os.Getpid(), nxt, cur, cur,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	return cmd.Start()
}
