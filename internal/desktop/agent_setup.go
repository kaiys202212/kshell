package desktop

import (
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// agentSetupNeeded 判断是否应弹出首次安装向导：已跳过则否；否则只要有「无 BinPath 且可一键安装」的工具。
func agentSetupNeeded(dismissed bool, tools []discovery.Tool, hasRecipe func(string) bool) bool {
	if dismissed {
		return false
	}
	for _, t := range tools {
		if t.BinPath != "" {
			continue
		}
		if hasRecipe(t.ID) {
			return true
		}
	}
	return false
}

// NeedsAgentSetup 在工具检测就绪后返回是否应弹出首次安装向导。
func (a *App) NeedsAgentSetup() bool {
	a.mu.Lock()
	dismissed := a.opts.Config.AgentSetupDismissed
	a.mu.Unlock()
	if dismissed {
		return false
	}
	tools := a.GetTools()
	return agentSetupNeeded(false, tools, func(id string) bool {
		_, _, err := a.lookupRecipe(id)
		return err == nil
	})
}

// DismissAgentSetup 落盘「已引导」，之后不再自动弹出向导。
func (a *App) DismissAgentSetup() error {
	return a.saveConfig(func(cfg *config.Config) {
		cfg.AgentSetupDismissed = true
	})
}
