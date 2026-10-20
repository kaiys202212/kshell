package desktop

import (
	"github.com/yangk/kshell/internal/providers"
)

// restoreSessionModel 在 ACP 恢复历史会话后，按磁盘 JSONL 尾部上次真实助手
// 模型尝试 SetModel。仅 Claude；模型不在可用列表、与 Current 相同、解析失败
// 或 SetModel 失败均静默跳过——激活本身已成功，不得挡用户继续对话。
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
	found := false
	for _, mo := range v.Models {
		if mo.ID == last {
			found = true
			break
		}
	}
	if !found || last == v.Current {
		return
	}
	m := a.chats()
	if m == nil {
		return
	}
	resolved, _ := a.resolveChatID(chatID)
	if err := m.SetModel(resolved, last); err != nil {
		return
	}
	// 与握手期 chat:models 推送同口径，前端下拉即时反映恢复后的 Current。
	if view, err := a.modelsView(resolved); err == nil {
		a.emitChatOut("chat:models", map[string]any{"id": chatID, "models": view})
	}
}
