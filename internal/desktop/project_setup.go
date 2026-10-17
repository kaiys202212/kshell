package desktop

// 首次项目选择弹窗：新用户首次扫描完成后，让用户勾选保留哪些扫描出的工作区，
// 未勾选的进回收站。与 AgentSetup 向导同款的一次性 dismissed 标记，但只对
// 「启动时 config.yaml 尚不存在」的全新用户生效，老用户永不弹。

import (
	"github.com/yangk/kshell/internal/config"
)

// NeedsProjectSetup 在首次扫描完成后返回是否应进入项目选择引导。
// 判定顺序：已落盘标记 → 老用户（启动时配置文件已存在）→ 未扫描完成，均不进。
// 工作区为空的情况返回 true，由前端拉到空列表后落盘标记放弃引导（本方法不做隐藏），
// 避免「NeedsProjectSetup 为 true 却没有工作区可选」的状态在两侧重复判定漂移。
func (a *App) NeedsProjectSetup() bool {
	a.mu.Lock()
	cfg := a.opts.Config
	existed := a.opts.ConfigFileExisted
	res := a.result
	a.mu.Unlock()
	if cfg.ProjectSetupDismissed {
		return false
	}
	if existed {
		return false
	}
	return res != nil // 首扫未完成时 false，前端等 scan:done 后再问
}

// DismissProjectSetup 落盘「已引导」，之后不再自动弹出项目选择弹窗。
// 空结果放弃引导、用户确认、用户直接关闭三种场景都走这里。
func (a *App) DismissProjectSetup() error {
	return a.saveConfig(func(cfg *config.Config) {
		cfg.ProjectSetupDismissed = true
	})
}
