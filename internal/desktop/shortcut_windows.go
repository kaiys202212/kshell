//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/executil"
)

func shortcutPlatformEnabled() bool { return true }

func userDesktopDir() (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "[Environment]::GetFolderPath('Desktop')")
	executil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("桌面路径为空")
	}
	return dir, nil
}

func desktopLnkPath() (string, error) {
	dir, err := userDesktopDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kshell.lnk"), nil
}

func createDesktopLnk(exe, lnk string) error {
	exeLit := strings.ReplaceAll(exe, "'", "''")
	lnkLit := strings.ReplaceAll(lnk, "'", "''")
	wdLit := strings.ReplaceAll(filepath.Dir(exe), "'", "''")
	ps := fmt.Sprintf(
		"$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s'); $s.TargetPath='%s'; $s.WorkingDirectory='%s'; $s.IconLocation='%s,0'; $s.Description='kshell'; $s.Save()",
		lnkLit, exeLit, wdLit, exeLit,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	executil.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建快捷方式: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a *App) ensureDesktopShortcutOnStartup() {
	if !shortcutPlatformEnabled() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	lnk, err := desktopLnkPath()
	if err != nil {
		return
	}
	o := a.snapshot()
	_ = ensureDesktopShortcut(shortcutDeps{
		ensured: o.Config.DesktopShortcutEnsured,
		exists: func() bool {
			_, err := os.Stat(lnk)
			return err == nil
		},
		create: func() error { return createDesktopLnk(exe, lnk) },
		mark: func() error {
			return a.saveConfig(func(cfg *config.Config) { cfg.DesktopShortcutEnsured = true })
		},
	})
}
