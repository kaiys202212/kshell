package desktop

import (
	"context"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
)

// AppearanceInfo 是暴露给前端的颜色模式快照。
type AppearanceInfo struct {
	Mode     string `json:"mode"`
	Resolved string `json:"resolved"`
}

func (a *App) currentAppearance() AppearanceInfo {
	mode := appearance.ParseMode(a.snapshot().Config.Appearance.Mode)
	return AppearanceInfo{Mode: string(mode), Resolved: string(appearance.Resolve(mode))}
}

// GetAppearance 返回当前模式与解析后的明暗（light/dark）。
func (a *App) GetAppearance() AppearanceInfo { return a.currentAppearance() }

// SetAppearanceMode 更新颜色模式并持久化，随后广播事件并重启系统监听。
func (a *App) SetAppearanceMode(mode string) error {
	m := appearance.ParseMode(mode)

	a.mu.Lock()
	if a.opts.Layout.Config == "" {
		a.mu.Unlock()
		return errNotReady
	}
	a.opts.Config.Appearance.Mode = string(m)
	cfg := a.opts.Config
	layout := a.opts.Layout
	a.mu.Unlock()

	if err := config.Save(layout, cfg); err != nil {
		return err
	}
	a.restartAppearanceWatcher()
	a.emitAppearance()
	return nil
}

// restartAppearanceWatcher 仅在 system 模式下启动注册表轮询；其它模式停掉。
func (a *App) restartAppearanceWatcher() {
	a.mu.Lock()
	if a.appearanceCancel != nil {
		a.appearanceCancel()
		a.appearanceCancel = nil
	}
	mode := appearance.ParseMode(a.opts.Config.Appearance.Mode)
	a.mu.Unlock()

	if mode != appearance.System {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
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
