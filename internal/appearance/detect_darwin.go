//go:build darwin

package appearance

import (
	"os/exec"
	"strings"

	"github.com/yangk/kshell/internal/executil"
)

// detectOSThemePlatform 读 macOS 全局外观：AppleInterfaceStyle 仅深色时存在
//（值为 Dark），浅色系统键缺失、defaults 以非零码退出。
func detectOSThemePlatform() Theme {
	cmd := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle")
	executil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ThemeLight
	}
	if strings.Contains(strings.ToLower(string(out)), "dark") {
		return ThemeDark
	}
	return ThemeLight
}
