//go:build windows

package workbuddy

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registryExecutable 从注册表卸载项兜底定位 WorkBuddy：用户可能把桌面端
// 装在任意盘符（本机即非标准目录），标准候选扫不到。卸载键名多为 GUID，
// 不含产品名，因此按 DisplayName 值匹配；exe 路径取 InstallLocation，
// 缺失时退回 DisplayIcon（形如 "D:\...\WorkBuddy.exe,0"）。
func registryExecutable() (string, bool) {
	roots := []struct {
		key registry.Key
		p   string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	for _, root := range roots {
		k, err := registry.OpenKey(root.key, root.p, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(root.key, root.p+`\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			display, _, _ := sub.GetStringValue("DisplayName")
			if !strings.Contains(display, "WorkBuddy") {
				sub.Close()
				continue
			}
			loc, _, errLoc := sub.GetStringValue("InstallLocation")
			icon, _, errIcon := sub.GetStringValue("DisplayIcon")
			sub.Close()
			var dirs []string
			if errLoc == nil && loc != "" {
				dirs = append(dirs, loc)
			}
			if errIcon == nil && icon != "" {
				icon = strings.SplitN(icon, ",", 2)[0]
				if icon != "" {
					dirs = append(dirs, filepath.Dir(icon))
				}
			}
			for _, dir := range dirs {
				for _, exe := range appExecutableNames {
					p := filepath.Join(dir, exe)
					if fileExists(p) {
						return p, true
					}
				}
			}
		}
	}
	return "", false
}
