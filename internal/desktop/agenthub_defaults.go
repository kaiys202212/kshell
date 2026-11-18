package desktop

import (
	"log"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
)

// AgentHub ⚙ 默认设置（config.yaml agenthub.default_tool/default_model）是
// 桌面端与微信链路共用的唯一权威：新建会话未显式指定工具/模型时都从这里取。
// 显式指定永远优先，本文的解析器只填空值。

// agenthubDefaultCreate 读取 ⚙ 默认设置并归一模型：claude 的 "default" 是
// 「让 agent 自选」的占位伪值，不能作为显式模型下发（Claude 网关会 400），
// 统一归一为空串（= 工具默认模型）。
func (a *App) agenthubDefaultCreate() (toolID, modelID string) {
	cfg := a.snapshot().Config.AgentHub
	toolID = strings.TrimSpace(cfg.DefaultTool)
	return toolID, normalizeCreateModelID(toolID, strings.TrimSpace(cfg.DefaultModel))
}

// normalizeCreateModelID 归一创建模型：只把 claude 的精确 "default"
// （trim + 大小写归一）视为未指定；其它工具可能真有同名模型，不拦。
func normalizeCreateModelID(toolID, modelID string) string {
	if modelID == "" {
		return ""
	}
	if toolID == "claude" && strings.EqualFold(modelID, "default") {
		return ""
	}
	return modelID
}

// findTool 按 ID 查扫描结果里的工具。
func findTool(tools []discovery.Tool, id string) (discovery.Tool, bool) {
	for _, t := range tools {
		if t.ID == id {
			return t, true
		}
	}
	return discovery.Tool{}, false
}

// toolUsableForCreate 判断工具能否作为新建目标：必须已安装；requireACP 时
// 还须 ACP 可用（终端路径只要求可执行入口）。
func toolUsableForCreate(t discovery.Tool, requireACP bool) bool {
	if !t.Installed {
		return false
	}
	if requireACP && !launch.ACPCapable(t) {
		return false
	}
	return true
}

// resolveCreateToolID 解析新建会话使用的工具：显式 toolID 原样返回（可用性
// 交给后续路径权威判定，保持既有报错语义）；空值取 ⚙ 默认工具；默认工具
// 未配置/不可用时**显式回落**首个已安装（且 ACP 可用）的工具。
// 回落分支带日志，便于排查「为什么不是我配置的工具」。
func (a *App) resolveCreateToolID(toolID string, requireACP bool) string {
	if toolID = strings.TrimSpace(toolID); toolID != "" {
		return toolID
	}
	tools := a.GetTools()
	defTool, _ := a.agenthubDefaultCreate()
	if defTool != "" {
		if t, ok := findTool(tools, defTool); ok && toolUsableForCreate(t, requireACP) {
			return defTool
		}
		log.Printf("agenthub 默认工具 %q 未安装或不可用，回落自动选择", defTool)
	} else {
		log.Printf("agenthub 默认设置未配置，回落首个已安装%s工具", acpSuffix(requireACP))
	}
	for _, t := range tools {
		if toolUsableForCreate(t, requireACP) {
			return t.ID
		}
	}
	return ""
}

// acpSuffix 只用于回落日志措辞。
func acpSuffix(requireACP bool) string {
	if requireACP {
		return "且 ACP 可用"
	}
	return ""
}

// resolveCreateModelID 解析新建会话使用的模型：显式 modelID 优先；空值时
// 仅当工具就是 ⚙ 默认工具才套用默认模型（模型 ID 与工具绑定，不能跨工具
// 复用）。claude 的 default 伪值按未指定处理。空串 = 工具自身默认。
func (a *App) resolveCreateModelID(toolID, modelID string) string {
	// 显式值（含 claude 的 "default" 伪值）不再套默认模型：伪值归一为空即
	// 「明确要求工具自选」，不能被 ⚙ 默认模型覆盖。
	if strings.TrimSpace(modelID) != "" {
		return normalizeCreateModelID(toolID, strings.TrimSpace(modelID))
	}
	defTool, defModel := a.agenthubDefaultCreate()
	if defModel != "" && strings.EqualFold(strings.TrimSpace(toolID), defTool) {
		return defModel
	}
	return ""
}
