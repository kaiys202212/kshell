package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yangk/kshell/internal/appearance"
)

// Theme 集中定义配色，避免样式散落在各面板里。
type Theme struct {
	Title     lipgloss.Style
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Header    lipgloss.Style
	Muted     lipgloss.Style
	Body      lipgloss.Style
	Preview   lipgloss.Style
	StatusBar lipgloss.Style
}

// NewTheme 按配置模式构建主题；system 模式跟随终端实际背景（比注册表更贴近 TUI 呈现环境）。
// NO_COLOR 优先级最高，直接降级为无色。
func NewTheme(mode appearance.Mode) Theme {
	if os.Getenv("NO_COLOR") != "" {
		// 全局降级，保证 Theme 之外新建的样式（列表、帮助、bubbles 组件）也不输出颜色。
		lipgloss.SetColorProfile(termenv.Ascii)
		return plainTheme()
	}

	theme := appearance.Resolve(mode)
	if mode == appearance.System {
		if lipgloss.HasDarkBackground() {
			theme = appearance.ThemeDark
		} else {
			theme = appearance.ThemeLight
		}
	}
	if theme == appearance.ThemeLight {
		return lightTheme()
	}
	return darkTheme()
}

func darkTheme() Theme {
	accent := lipgloss.Color("39")
	dim := lipgloss.Color("245")
	fg := lipgloss.Color("252")

	return Theme{
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(accent).Bold(true),
		Tab:       lipgloss.NewStyle().Foreground(dim),
		TabActive: lipgloss.NewStyle().Foreground(fg).Bold(true),
		Header:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		Muted:     lipgloss.NewStyle().Foreground(dim),
		Body:      lipgloss.NewStyle().Foreground(fg),
		Preview:   lipgloss.NewStyle().Foreground(fg),
		StatusBar: lipgloss.NewStyle().Foreground(dim),
	}
}

// lightTheme 面向浅色终端背景：深灰正文、更深的主色，标题白字配蓝底。
func lightTheme() Theme {
	accent := lipgloss.Color("26")
	dim := lipgloss.Color("243")
	fg := lipgloss.Color("235")

	return Theme{
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(accent).Bold(true),
		Tab:       lipgloss.NewStyle().Foreground(dim),
		TabActive: lipgloss.NewStyle().Foreground(fg).Bold(true),
		Header:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		Muted:     lipgloss.NewStyle().Foreground(dim),
		Body:      lipgloss.NewStyle().Foreground(fg),
		Preview:   lipgloss.NewStyle().Foreground(fg),
		StatusBar: lipgloss.NewStyle().Foreground(dim),
	}
}

// plainTheme 供 NO_COLOR 环境使用：保留粗体层级，不输出任何颜色转义。
func plainTheme() Theme {
	plain := lipgloss.NewStyle()
	return Theme{
		Title:     plain.Bold(true),
		Tab:       plain,
		TabActive: plain.Bold(true),
		Header:    plain.Bold(true),
		Muted:     plain,
		Body:      plain,
		Preview:   plain,
		StatusBar: plain,
	}
}
