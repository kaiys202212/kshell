package desktop

import (
	"strings"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/providers"
)

// modelIDLooseEqual 比较模型 ID：字面相等，或把 '.' 归一成 '-' 后相等。
// Claude/网关常把配置里的 glm-5.3-flash 写进 JSONL 成 glm-5-3-flash。
func modelIDLooseEqual(a, b string) bool {
	if a == b {
		return true
	}
	return strings.ReplaceAll(a, ".", "-") == strings.ReplaceAll(b, ".", "-")
}

// restoreModelBareID 多平台前缀 ID 剥成裸名；非前缀原样返回。
func restoreModelBareID(id string) string {
	if _, model, ok := splitKshellModelID(id); ok {
		return model
	}
	return id
}

// resolveRestoreModelID 在可用列表中找与 JSONL last 对应、可下发给 SetModel 的裸名。
// 先精确再点号/连字符宽松；多平台比裸名。已与 Current 宽松相同或找不到返回 ""。
func resolveRestoreModelID(last string, models []chat.ModelView, current string) string {
	if last == "" || modelIDLooseEqual(last, current) {
		return ""
	}
	accept := func(bare, id string) string {
		if modelIDLooseEqual(bare, current) || bare == current || id == current || modelIDLooseEqual(id, current) {
			return ""
		}
		return bare
	}
	for _, mo := range models {
		bare := restoreModelBareID(mo.ID)
		if mo.ID == last || bare == last {
			return accept(bare, mo.ID)
		}
	}
	for _, mo := range models {
		bare := restoreModelBareID(mo.ID)
		if modelIDLooseEqual(mo.ID, last) || modelIDLooseEqual(bare, last) {
			return accept(bare, mo.ID)
		}
	}
	return ""
}

// restoreSessionModel 在 ACP 恢复历史会话后，按磁盘 JSONL 尾部上次真实助手
// 模型尝试 SetModel。仅 Claude；模型不在可用列表、与 Current 相同、解析失败
// 或 SetModel 失败均静默跳过——激活本身已成功，不得挡用户继续对话。
// JSONL 与列表仅点号/连字符差异时按列表规范裸名恢复（见 resolveRestoreModelID）。
func (a *App) restoreSessionModel(chatID, toolID, sessionPath string) {
	if toolID != "claude" || sessionPath == "" || chatID == "" {
		return
	}
	last := providers.ClaudeLastAssistantModel(sessionPath)
	if last == "" {
		return
	}
	v, err := a.modelsView(chatID)
	if err != nil {
		return
	}
	target := resolveRestoreModelID(last, v.Models, v.Current)
	if target == "" {
		return
	}
	m := a.chats()
	if m == nil {
		return
	}
	resolved, _ := a.resolveChatID(chatID)
	if err := m.SetModel(resolved, target); err != nil {
		return
	}
	// 与握手期 chat:models 推送同口径，前端下拉即时反映恢复后的 Current。
	if view, err := a.modelsView(resolved); err == nil {
		a.emitChatOut("chat:models", map[string]any{"id": chatID, "models": view})
	}
}
