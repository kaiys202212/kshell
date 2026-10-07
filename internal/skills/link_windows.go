//go:build windows

package skills

import (
	"fmt"
	"os/exec"
	"syscall"
)

func windowsJunction(entityDir, targetDir string) error {
	// mklink /J 是 cmd 内建；/C 后整段作为命令。
	cmd := exec.Command("cmd", "/C", "mklink", "/J", targetDir, entityDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("junction: %w: %s", err, string(out))
	}
	return nil
}
