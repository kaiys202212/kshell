//go:build windows

package appearance

import "golang.org/x/sys/windows/registry"

// detectOSThemePlatform 读注册表 AppsUseLightTheme：1=浅色、0=深色；读不到按深色。
func detectOSThemePlatform() Theme {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ThemeDark
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil || v == 0 {
		return ThemeDark
	}
	return ThemeLight
}
