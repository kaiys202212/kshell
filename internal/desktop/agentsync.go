package desktop

import (
	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// syncAgentModels 把当前模型平台写入已安装（或已有配置）的 OpenCode/CodeBuddy/Claude。
// 失败只记日志语义：调用方不因同步失败回滚配置保存。
func (a *App) syncAgentModels() {
	o := a.snapshot()
	_ = agentsync.SyncAll(o.Home, agentsync.InputFromModel(o.Config.Model), installedToolIDs(a.toolsSnapshot()))
}

// syncAgentModelsAfterSave 处理模型配置保存后的落盘：通常按新配置同步；但若
// 本次保存把最后一点 kshell 托管痕迹也删掉了（删光平台并禁用），必须先用旧
// 平台做一次「只清不建」的同步，否则历史注入物会永久残留、指向已删端点。
// 不能用「永久 managed 标记」代替：已回退用户每次启动都会被清掉 ccswitch 配置。
func (a *App) syncAgentModelsAfterSave(prev config.ModelConfig) {
	o := a.snapshot()
	next := agentsync.InputFromModel(o.Config.Model)
	if !next.Managed() && agentsync.InputFromModel(prev).Managed() {
		cleanup := agentsync.SyncInput{Enabled: false, Platforms: prev.Platforms}
		_ = agentsync.SyncAll(o.Home, cleanup, installedToolIDs(a.toolsSnapshot()))
		return
	}
	_ = agentsync.SyncAll(o.Home, next, installedToolIDs(a.toolsSnapshot()))
}

// syncAgentModelTool 安装成功后只同步刚装上的工具。
func (a *App) syncAgentModelTool(toolID string) {
	o := a.snapshot()
	_ = agentsync.SyncTool(o.Home, toolID, agentsync.InputFromModel(o.Config.Model))
}

func installedToolIDs(tools []discovery.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t.Installed {
			out = append(out, t.ID)
		}
	}
	return out
}
