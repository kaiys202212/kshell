//go:build !windows && !darwin

package appearance

// detectOSThemePlatform Linux 等其它平台暂无系统级 API，兜底深色
//（TUI 另有终端背景探测）；darwin 实现见 detect_darwin.go。
func detectOSThemePlatform() Theme { return ThemeDark }
