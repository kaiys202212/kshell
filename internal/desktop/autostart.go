// 开机自启动的桌面端绑定：AutostartInfo 供设置页读取状态、SetAutostart 供开关切换，
// syncAutostartOnStartup 在 Startup 末尾把注册表与 config.Autostart 对齐
//（发布版首次运行即生效，并自愈 exe 挪位后的过期路径）。
// dev 构建整体视为不支持：不查询、不注册、不拦截，UI 侧隐藏。
package desktop

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/yangk/kshell/internal/autostart"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/version"
)

// 注入点（包级函数变量，对齐 launchPrepDeps 式抽象）：注册表操作与发布态判定
// 均可替换，测试真打桩、生产走 internal/autostart 与 version.IsRelease。
var (
	isReleaseFn          = version.IsRelease
	autostartSupportedFn = autostart.Supported
	autostartEnableFn    = autostart.Enable
	autostartDisableFn   = autostart.Disable
	autostartEnabledFn   = autostart.Enabled
)

var errAutostartUnsupported = errors.New("err.desktop.autostart_unsupported")

// AutostartInfoView 是设置页的自启动状态：Supported 为假时整体隐藏该设置项，
// Enabled 反映注册表当前状态。
type AutostartInfoView struct {
	Supported bool
	Enabled   bool
}

// AutostartInfo 返回自启动支持与开启状态。dev 构建 / 非 Windows 平台返回
// 全假且不触碰注册表；注册表读取失败按未开启处理（仅记日志，不影响渲染）。
func (a *App) AutostartInfo() AutostartInfoView {
	if !autostartSupportedFn() || !isReleaseFn() {
		return AutostartInfoView{}
	}
	enabled, err := autostartEnabledFn()
	if err != nil {
		log.Printf("查询自启动注册表状态失败: %v", err)
		return AutostartInfoView{Supported: true}
	}
	return AutostartInfoView{Supported: true, Enabled: enabled}
}

// SetAutostart 切换开机自启动：先写注册表（Enable 用当前 exe 路径），成功后再
// 更新 config.Autostart 并落盘——顺序保证不落半态；config 落盘失败时下次
// Startup 的同步会按注册表现状自愈。不支持时返回 wire 错误且不动任何状态。
func (a *App) SetAutostart(on bool) error {
	if !autostartSupportedFn() || !isReleaseFn() {
		return errAutostartUnsupported
	}
	if err := applyAutostart(on); err != nil {
		return fmt.Errorf("err.desktop.autostart_failed|%v", err)
	}
	return a.saveConfig(func(cfg *config.Config) { cfg.Autostart = on })
}

// markStartHidden 在 Startup 装配完依赖后同步 windowHidden 标志：
// StartHidden 静默拉起时窗口实际已隐藏，标志若滞后为 false，
// 热键/通知的「窗口是否可见」分流会误判为可见。
func (a *App) markStartHidden() {
	if a.snapshot().StartHidden {
		a.setWindowHidden(true)
	}
}

// applyAutostart 把注册表对齐到目标状态：开→Enable（当前 exe 路径），关→Disable。
func applyAutostart(on bool) error {
	if on {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("取当前可执行文件路径失败: %w", err)
		}
		return autostartEnableFn(exe)
	}
	return autostartDisableFn()
}

// syncAutostartOnStartup 在 Startup 末尾对齐注册表与 config.Autostart：
// 仅 release 且平台支持、config 已就绪时执行（dev/不支持/config 未就绪则
// 完全不动注册表——initRealDeps 早退时 opts.Config 是零值，按它同步会把
// 用户注册误删成 Disable）；失败仅写启动日志，绝不阻断启动——下次启动会再同步。
func (a *App) syncAutostartOnStartup() {
	if !isReleaseFn() || !autostartSupportedFn() {
		return
	}
	if a.snapshot().Layout.Config == "" { // config 未就绪（依赖装配早退），不得按零值配置同步
		return
	}
	on := a.snapshot().Config.Autostart
	if err := applyAutostart(on); err != nil {
		log.Printf("开机自启动同步失败 (on=%v): %v", on, err)
	}
}
