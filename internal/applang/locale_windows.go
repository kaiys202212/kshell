//go:build windows

package applang

import "golang.org/x/sys/windows"

// windowsUserLocale 返回用户首选 UI 语言名（如 zh-CN、en-US），取不到时返回空串。
func windowsUserLocale() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return ""
	}
	return langs[0]
}
