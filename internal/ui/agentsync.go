package ui

import (
	"strings"

	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// syncAgentModels 把平台模型写入各工具自有配置：Claude settings.json、
// OpenCode opencode.json、CodeBuddy models.json。只在 TUI 启动首轮扫描前调用一次，
// 与桌面端「首轮探测后同步」同构。失败静默：同步是附带的落盘动作，不得阻断启动与扫描。
func syncAgentModels(home string, mc config.ModelConfig, tools []discovery.Tool) {
	if home == "" || !modelManaged(mc) {
		return
	}
	_ = agentsync.SyncAll(home, agentsync.InputFromModel(mc), installedToolIDs(tools))
}

// modelManaged 报告配置里是否留有 kshell 平台模型的痕迹。TUI 不是模型配置入口：
// 从未配过的用户（默认 Enabled=false、无平台）启动 TUI 不得触发同步——agentsync
// 「先删后建」会清掉用户手写或 ccswitch 写入的 ANTHROPIC_*。只要还有平台、默认平台
// 或 ActiveModel，就视为 kshell 托管过，此时禁用也要清理。
func modelManaged(mc config.ModelConfig) bool {
	return mc.Enabled ||
		len(mc.Platforms) > 0 ||
		strings.TrimSpace(mc.DefaultPlatform) != "" ||
		strings.TrimSpace(mc.ActiveModel) != ""
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
