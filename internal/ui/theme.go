package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
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

func NewTheme() Theme {
	if os.Getenv("NO_COLOR") != "" {
		return plainTheme()
	}

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
