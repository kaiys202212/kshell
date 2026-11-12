package desktop

import (
	"strings"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/modelresolve"
	"github.com/yangk/kshell/internal/providers"
)

// restoreModelBareID 多平台前缀 ID 剥成裸名；非前缀原样返回。
func restoreModelBareID(id string) string {
	if _, model, ok := modelresolve.SplitKshellModelID(id); ok {
		return model
	}
	return id
}

// resolveRestoreModelID 在可用列表中找与 JSONL last 对应、可下发给 SetModel 的裸名。
// 先精确再点号/连字符宽松；多平台比裸名。已与 Current 宽松相同或找不到返回 ""。
func resolveRestoreModelID(last string, models []chat.ModelView, current string) string {
	if last == "" || modelresolve.ModelIDLooseEqual(last, current) {
		return ""
	}
	accept := func(bare, id string) string {
		if modelresolve.ModelIDLooseEqual(bare, current) || bare == current || id == current || modelresolve.ModelIDLooseEqual(id, current) {
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
		if modelresolve.ModelIDLooseEqual(mo.ID, last) || modelresolve.ModelIDLooseEqual(bare, last) {
			return accept(bare, mo.ID)
		}
	}
	return ""
}

// restoreCandidates 构造恢复模型的候选项：与下拉合并不同，恢复不依赖按工具
// 同步开关（ToolSync）——它只是把 JSONL/state 里的模型名映射回配置规范名，
// 取全部平台模型（裸名 ID）。
func (a *App) restoreCandidates() []chat.ModelView {
	cfg := a.snapshot().Config.Model
	if !cfg.Enabled {
		return nil
	}
	var out []chat.ModelView
	for _, p := range cfg.Platforms {
		for _, m := range p.Models {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			out = append(out, chat.ModelView{ID: m, Name: platformModelDisplayName(p.Name, m)})
		}
	}
	return out
}

// restoreSessionModel 在 ACP 恢复历史会话后尝试恢复上次模型：
//   - claude：按磁盘 JSONL 尾部上次真实助手模型；
//   - opencode：OpenCode 的会话不记模型（resume 后 Current 恒为 agent 默认），
//     改查 kshell 侧 state 记忆（SetChatModel 成功时落盘，见 session_model_state.go）。
//
// 模型不在可用列表、与 Current 相同、解析失败或 SetModel 失败均静默跳过
// ——激活本身已成功，不得挡用户继续对话。last 与列表仅点号/连字符差异时
// 按列表规范 ID 恢复（见 resolveRestoreModelID）。
func (a *App) restoreSessionModel(chatID, toolID, sessionID, sessionPath string) {
	var last string
	switch toolID {
	case "claude":
		if sessionPath == "" || chatID == "" {
			return
		}
		last = providers.ClaudeLastAssistantModel(sessionPath)
	case "opencode":
		if sessionID == "" {
			return
		}
		last = a.lookupOpenCodeSessionModel(sessionID)
	default:
		return
	}
	if last == "" {
		return
	}
	m := a.chats()
	if m == nil {
		return
	}
	resolved, _ := a.resolveChatID(chatID)
	av, ok := m.Models(resolved)
	if !ok {
		av = chat.ModelsView{Models: []chat.ModelView{}}
	}
	models := append(append([]chat.ModelView{}, av.Models...), a.restoreCandidates()...)
	target := resolveRestoreModelID(last, models, av.Current)
	if target == "" {
		return
	}
	if err := m.SetModel(resolved, target); err != nil {
		return
	}
	// 与握手期 chat:models 推送同口径，前端下拉即时反映恢复后的 Current。
	if view, err := a.modelsView(resolved); err == nil {
		a.emitChatOut("chat:models", map[string]any{"id": chatID, "models": view})
	}
}
