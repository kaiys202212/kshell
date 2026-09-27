package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/ui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 无法确定用户主目录:", err)
		os.Exit(1)
	}

	paths, err := config.Paths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kshell:", err)
		os.Exit(1)
	}

	cfg, cfgErr := config.Load(paths)
	if err := config.EnsureRoot(paths); err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 无法创建配置目录:", err)
		os.Exit(1)
	}

	model := ui.NewModelWith(ui.Options{
		Home:      home,
		Config:    cfg,
		CachePath: paths.CacheIndex,
		Providers: []providers.Provider{providers.Claude{}, providers.Codex{}, providers.Gemini{}},
	})

	// 配置加载问题只作为提示，不阻断启动。
	if cfgErr != nil {
		model = model.WithStatus(cfgErr.Error(), true)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "kshell 启动失败:", err)
		os.Exit(1)
	}
}
