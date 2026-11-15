package desktop

import (
	"log"
	"os"

	"github.com/yangk/kshell/internal/clisync"
	"github.com/yangk/kshell/internal/version"
)

// clisync.go 桌面端接线：启动时后台释放最小 agent CLI（幂等哈希门控），
// 设置页按钮按宿主列表触发全量同步（释放 + PATH + 各宿主技能）。

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

// ensureCliSync 是启动路径的后台释放：释放二进制并自愈已装过的宿主技能，
// 绝不改 PATH（环境变量变更必须用户在设置页显式触发）、不扩散新宿主。
// 失败仅记日志。
func (a *App) ensureCliSync() {
	home := a.clisyncHome()
	if home == "" {
		return
	}
	if _, err := clisync.Sync(clisync.Options{
		Home:    home,
		Binary:  a.opts.CLIBinary,
		Version: version.Current(),
		// 启动自愈：只刷新已装过 kshell 技能的宿主；全新环境只释放 CLI，
		// 装到哪个宿主由用户在设置页显式决定。
		Targets: clisync.ExistingSkillTargets(home),
	}); err != nil {
		log.Printf("clisync 启动释放失败: %v", err)
	}
}

// GetHostSyncStatuses 返回各宿主同步状态（只读，不写盘；Skills 覆盖全部
// 已知宿主，前端按检测到的宿主筛选展示）。
func (a *App) GetHostSyncStatuses() clisync.Result {
	return clisync.Status(clisync.Options{
		Home:       a.clisyncHome(),
		Binary:     a.opts.CLIBinary,
		PathLookup: a.opts.ClisyncPathEntries,
	})
}

// SyncHosts 执行设置页「同步」：释放 CLI + 配置 PATH + 安装技能到指定宿主。
// 单宿主失败记入结果字段；PATH 配置走可注入实现（测试打桩）。
func (a *App) SyncHosts(toolIDs []string) (clisync.Result, error) {
	return clisync.Sync(clisync.Options{
		Home:          a.clisyncHome(),
		Binary:        a.opts.CLIBinary,
		Version:       version.Current(),
		Targets:       toolIDs,
		ConfigurePath: true,
		EnsurePath:    a.opts.ClisyncEnsurePath,
		PathLookup:    a.opts.ClisyncPathEntries,
	})
}
