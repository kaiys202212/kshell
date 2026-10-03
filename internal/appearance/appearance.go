// Package appearance 是 kshell 的颜色模式唯一来源：解析用户配置的模式、探测操作系统明暗、
// 监听系统变化，并向各界面/启动器提供通用颜色环境变量。各界面只消费 Theme，不自行判断。
package appearance

import "strings"

// Mode 是用户可配置的颜色模式。
type Mode string

const (
	System Mode = "system"
	Light  Mode = "light"
	Dark   Mode = "dark"
)

// Theme 是解析后的明暗结果。
type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// ParseMode 归一化模式字符串；非法或空值一律回落 system。
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(Light):
		return Light
	case string(Dark):
		return Dark
	default:
		return System
	}
}

// detectOSTheme 是可替换的探测入口：windows 读注册表，其它平台兜底深色。
// 抽成变量便于单测注入。
var detectOSTheme = detectOSThemePlatform

// Resolve 把模式解析为具体明暗：light/dark 直接返回，system 走 OS 探测。
func Resolve(mode Mode) Theme {
	switch mode {
	case Light:
		return ThemeLight
	case Dark:
		return ThemeDark
	default:
		return detectOSTheme()
	}
}

// GenericEnv 返回对所有工具通用的颜色环境变量：
// COLORFGBG（前景;背景，背景 0-6/8 为暗、7/9-15 为亮）与 COLORTERM=truecolor。
func GenericEnv(t Theme) map[string]string {
	fg, bg := "15", "0"
	if t == ThemeLight {
		fg, bg = "0", "15"
	}
	return map[string]string{
		"COLORFGBG": fg + ";" + bg,
		"COLORTERM": "truecolor",
	}
}
