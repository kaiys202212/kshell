package desktop

// 首次项目选择弹窗：新用户首次扫描完成后，让用户勾选保留哪些扫描出的工作区，
// 未勾选的进回收站。与 AgentSetup 向导同款的一次性 dismissed 标记，但只对
// 「启动时 config.yaml 尚不存在」的全新用户生效，老用户永不弹。

import (
	"github.com/yangk/kshell/internal/config"
)

// NeedsProjectSetup 在首次扫描完成后返回是否应弹出项目选择弹窗。
// 判定顺序：已落盘标记 → 老用户（启动时配置文件已存在）→ 未扫描完成 → 工作区为空，均不弹。
func (a *App) NeedsProjectSetup() bool {
	a.mu.Lock()
	cfg := a.opts.Config
	existed := a.opts.ConfigFileExisted
	a.mu.Unlock()
	if cfg.ProjectSetupDismissed {
		return false
	}
	if existed {
		return false
	}
	a.mu.Lock()
	res := a.result
	a.mu.Unlock()
	if res == nil {
		return false // 首扫未完成，前端等 scan:done 后再问
	}
	return len(a.GetWorkspaces()) > 0
}

// DismissProjectSetup 落盘「已引导」，之后不再自动弹出项目选择弹窗。
// 空结果放弃引导、用户确认、用户直接关闭三种场景都走这里。
func (a *App) DismissProjectSetup() error {
	return a.saveConfig(func(cfg *config.Config) {
		cfg.ProjectSetupDismissed = true
	})
}
