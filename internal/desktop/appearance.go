package desktop

import (
	"context"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
)

// AppearanceInfo 是暴露给前端的颜色模式快照。
type AppearanceInfo struct {
	Mode           string `json:"mode"`
	Resolved       string `json:"resolved"`
	FontSize       int    `json:"fontSize"`
	ShowWhitespace bool   `json:"showWhitespace"`
}

func (a *App) currentAppearance() AppearanceInfo {
	cfg := a.snapshot().Config.Appearance
	mode := appearance.ParseMode(cfg.Mode)
	return AppearanceInfo{
		Mode:           string(mode),
		Resolved:       string(appearance.Resolve(mode)),
		FontSize:       config.ClampUIFontSize(cfg.FontSize),
		ShowWhitespace: cfg.ShowWhitespace,
	}
}

// GetAppearance 返回当前模式与解析后的明暗（light/dark）。
func (a *App) GetAppearance() AppearanceInfo { return a.currentAppearance() }

// SetAppearanceMode 更新颜色模式并持久化，随后广播事件并重启系统监听。
// 先落盘成功再更新内存：Save 失败时内存保持原值，避免界面与磁盘不一致。
func (a *App) SetAppearanceMode(mode string) error {
	m := appearance.ParseMode(mode)
	if err := a.saveConfig(func(cfg *config.Config) { cfg.Appearance.Mode = string(m) }); err != nil {
		return err
	}
	a.restartAppearanceWatcher()
	a.emitAppearance()
	return nil
}

// SetAppearanceFontSize 更新全局 UI 字号并持久化，随后广播 appearance:changed。
func (a *App) SetAppearanceFontSize(n int) error {
	n = config.ClampUIFontSize(n)
	if err := a.saveConfig(func(cfg *config.Config) {
		cfg.Appearance.FontSize = n
	}); err != nil {
		return err
	}
	a.emitAppearance()
	return nil
}

// SetAppearanceShowWhitespace 更新编辑器空白字符显示并持久化，随后广播 appearance:changed。
func (a *App) SetAppearanceShowWhitespace(show bool) error {
	if err := a.saveConfig(func(cfg *config.Config) {
		cfg.Appearance.ShowWhitespace = show
	}); err != nil {
		return err
	}
	a.emitAppearance()
	return nil
}

// restartAppearanceWatcher 仅在 system 模式下启动注册表轮询；其它模式停掉。
// 取消旧监听、判断模式、装新 cancel 必须在同一把锁内完成，否则并发调用会
// 在解锁窗口里交错，导致新监听被旧调用清掉或反之（竞态）。
func (a *App) restartAppearanceWatcher() {
	a.mu.Lock()
	if a.appearanceCancel != nil {
		a.appearanceCancel()
		a.appearanceCancel = nil
	}
	if appearance.ParseMode(a.opts.Config.Appearance.Mode) != appearance.System {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.appearanceCancel = cancel
	a.mu.Unlock()

	go appearance.NewWatcher(func(appearance.Theme) { a.emitAppearance() }).Run(ctx)
}

// emitAppearance 把当前明暗推给原生窗口与前端。
func (a *App) emitAppearance() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()

	info := a.currentAppearance()
	applyNativeTheme(ctx, info.Resolved)
	a.Emit("appearance:changed", info)
}
