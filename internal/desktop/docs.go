package desktop

import (
	"github.com/yangk/kshell/internal/appdocs"
	"github.com/yangk/kshell/internal/config"
)

// GetHelpDoc 返回帮助使用说明（lang 仅 zh-CN，其余一律回退英文版）。
// kind 在此层写死为 "help"，不含注入面。
func (a *App) GetHelpDoc(lang string) string { return appdocs.Get("help", lang) }

// GetChangelogDoc 返回升级日志（lang 口径同上）。
func (a *App) GetChangelogDoc(lang string) string { return appdocs.Get("changelog", lang) }

// GetLastSeenVersion 返回用户已确认过的最新版本号（空串表示尚未确认过）。
func (a *App) GetLastSeenVersion() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts.Config.LastSeenVersion
}

// MarkVersionSeen 持久化用户已确认的版本号，用于升级后弹升级日志的去重。
func (a *App) MarkVersionSeen(version string) error {
	return a.saveConfig(func(cfg *config.Config) { cfg.LastSeenVersion = version })
}
