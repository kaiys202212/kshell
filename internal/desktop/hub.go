package desktop

import (
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/hub"
)

// recordHubMessage 把一条消息写入 AgentHub 历史并通知前端刷新。
// 这是用户消息与 agent 回复共用的唯一写入路径：两类消息进同一 store、同一 ID 序列，
// 前端按 ID 排序即得一致时序。未装配 store（未就绪/测试）时静默跳过。
// 写入失败只影响历史留存，不打断主流程（发送/通知不应因落盘失败而失败），
// 但仍 emit 让前端拿到已写入的部分（Append 失败时 live 未推进，前端重拉即最新状态）。
func (a *App) recordHubMessage(m hub.Message) {
	store := a.snapshot().HubStore
	if store == nil {
		return
	}
	_ = store.Append(m)
	a.Emit("hub:changed")
}

// recordHubUserMessage 记录一条用户发出的消息：从 chat manager 反查归因字段。
// chatID 不存在（会话已关闭）时仍记录文本，只是 workspace/tool 缺失。
func (a *App) recordHubUserMessage(chatID, text string) {
	if text == "" {
		return
	}
	m := hub.Message{Kind: "user", ChatID: chatID, Text: text}
	if mgr := a.chats(); mgr != nil {
		if info, ok := mgr.Get(chatID); ok {
			m.Tool = info.ToolID
			m.Workspace = info.Workspace
		}
	}
	a.recordHubMessage(m)
}

// recordHubAgentMessage 记录一条 agent 回复通知（hook/OSC 与 ACP 两条通道共用）。
func (a *App) recordHubAgentMessage(p agenthook.Payload) {
	a.recordHubMessage(hub.Message{
		Kind:      "agent",
		Tool:      p.Tool,
		TermKey:   p.TermKey,
		Workspace: p.Workspace,
		Text:      p.Summary,
		Event:     p.Event,
		Ts:        p.Ts,
	})
}

// ListHubMessages 返回 AgentHub 最近的消息历史（live 部分，最多 500 条）。
func (a *App) ListHubMessages() []hub.Message {
	store := a.snapshot().HubStore
	if store == nil {
		return []hub.Message{}
	}
	return store.List()
}

// ListHubArchive 返回归档月份列表（新在前），供前端归档入口展示。
func (a *App) ListHubArchive() []hub.ArchiveMonth {
	store := a.snapshot().HubStore
	if store == nil {
		return []hub.ArchiveMonth{}
	}
	return store.ListArchive()
}

// LoadHubArchive 读取某月归档全量消息。
func (a *App) LoadHubArchive(month string) ([]hub.Message, error) {
	store := a.snapshot().HubStore
	if store == nil {
		return []hub.Message{}, nil
	}
	return store.LoadArchive(month)
}

// ClearHubMessages 清空 AgentHub 的 live 消息（归档保留）；供 /clear 客户端命令。
func (a *App) ClearHubMessages() error {
	store := a.snapshot().HubStore
	if store == nil {
		return nil
	}
	if err := store.Clear(); err != nil {
		return err
	}
	a.Emit("hub:changed")
	return nil
}
