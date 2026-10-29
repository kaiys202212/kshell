package ui

import (
	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// syncAgentModels 把平台模型写入各工具自有配置：Claude settings.json、
// OpenCode opencode.json、CodeBuddy models.json。只在 TUI 启动首轮扫描前调用一次，
// 与桌面端「首轮探测后同步」同构。失败静默：同步是附带的落盘动作，不得阻断启动与扫描。
//
// 是否该动各工具配置由 agentsync.SyncInput.Managed 统一判定：从未配置过模型时
// 各工具文件原样保留，只保留 Claude 登录标记处理，避免清掉 ccswitch 等第三方配置。
func syncAgentModels(home string, mc config.ModelConfig, tools []discovery.Tool) {
	if home == "" {
		return
	}
	_ = agentsync.SyncAll(home, agentsync.InputFromModel(mc), installedToolIDs(tools))
}

// installedToolIDs 取已安装工具 ID；SyncAll 内部还会用「配置文件已存在」兜底。
func installedToolIDs(tools []discovery.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t.Installed {
			out = append(out, t.ID)
		}
	}
	return out
}
