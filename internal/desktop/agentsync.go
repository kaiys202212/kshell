package desktop

import (
	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/discovery"
)

// syncAgentModels 把当前模型平台写入已安装（或已有配置）的 OpenCode/CodeBuddy。
// 失败只记日志语义：调用方不因同步失败回滚配置保存。
func (a *App) syncAgentModels() {
	o := a.snapshot()
	_ = agentsync.SyncAll(o.Home, agentsync.InputFromModel(o.Config.Model), installedToolIDs(a.toolsSnapshot()))
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
