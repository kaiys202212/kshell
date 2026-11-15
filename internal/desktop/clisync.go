package desktop

import (
	"log"
	"os"

	"github.com/yangk/kshell/internal/clisync"
	"github.com/yangk/kshell/internal/version"
)

// clisync.go 桌面端接线：启动时后台释放最小 agent CLI（幂等哈希门控），
// 设置页按钮触发全量同步（释放 + PATH + WorkBuddy 技能）。

// clisyncHome 返回同步用的用户主目录：优先注入的 opts.Home（测试），
// 缺失回落 os.UserHomeDir。
func (a *App) clisyncHome() string {
	if a.opts.Home != "" {
		return a.opts.Home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// ensureCliSync 是启动路径的后台释放：只放二进制与技能文档，绝不改 PATH
//（环境变量变更必须用户在设置页显式触发）。失败仅记日志。
func (a *App) ensureCliSync() {
	home := a.clisyncHome()
	if home == "" {
		return
	}
	if _, err := clisync.Sync(clisync.Options{
		Home:    home,
		Binary:  a.opts.CLIBinary,
		Version: version.Current(),
	}); err != nil {
		log.Printf("clisync 启动释放失败: %v", err)
	}
}

// GetWorkBuddySyncStatus 返回当前同步状态（只读，不写盘）。
func (a *App) GetWorkBuddySyncStatus() clisync.Result {
	return clisync.Status(clisync.Options{
		Home:       a.clisyncHome(),
		Binary:     a.opts.CLIBinary,
		PathLookup: a.opts.ClisyncPathEntries,
	})
}

// SyncWorkBuddy 执行设置页「同步到 WorkBuddy」：释放 CLI + 配置 PATH + 安装
// 技能。单步失败记入结果字段；PATH 配置走可注入实现（测试打桩）。
func (a *App) SyncWorkBuddy() (clisync.Result, error) {
	return clisync.Sync(clisync.Options{
		Home:          a.clisyncHome(),
		Binary:        a.opts.CLIBinary,
		Version:       version.Current(),
		ConfigurePath: true,
		EnsurePath:    a.opts.ClisyncEnsurePath,
		PathLookup:    a.opts.ClisyncPathEntries,
	})
}
