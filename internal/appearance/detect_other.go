//go:build !windows

package appearance

// detectOSThemePlatform 非 Windows 平台暂无系统级 API，兜底深色（TUI 另有终端背景探测）。
func detectOSThemePlatform() Theme { return ThemeDark }
