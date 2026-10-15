package desktop

import (
	"errors"

	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/hub"
)

// Hub 撤回相关错误（wire key，前端 translateBackend 翻译）。
var (
	errHubNotFound  = errors.New("err.hub.not_found")
	errHubNotQueued = errors.New("err.hub.not_queued")
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
	a.recordHubUserMessageStatus(chatID, text, "", "")
}

// recordHubUserMessageStatus 同上，但带投递状态（queued / 空）；hubID 非空时用预分配 ID。
func (a *App) recordHubUserMessageStatus(chatID, text, hubID, status string) {
	if text == "" {
		return
	}
	m := hub.Message{Kind: "user", ChatID: chatID, Text: text, Status: status}
	if mgr := a.chats(); mgr != nil {
		if info, ok := mgr.Get(chatID); ok {
			m.Tool = info.ToolID
			m.Workspace = info.Workspace
		}
	}
	a.recordHubMessageWithID(m, hubID)
}

// recordHubMessageWithID 落盘一条消息；hubID 非空时解析为数值用 AppendWithID
//（发送链路已把该 ID 交给 chat 队列，撤回靠它定位），否则走普通 Append。
func (a *App) recordHubMessageWithID(m hub.Message, hubID string) {
	store := a.snapshot().HubStore
	if store == nil {
		return
	}
	if hubID != "" {
		if id, ok := parseHubID(hubID); ok {
			m.ID = id
			_ = store.AppendWithID(m)
			a.Emit("hub:changed")
			return
		}
	}
	_ = store.Append(m)
	a.Emit("hub:changed")
}

// recordHubAck 落一条模拟 agent 确认卡片：Text/Event 都用稳定 wire key，
// 前端经 translateBackend 翻译；归因从该 hubID 对应的用户消息反查。
func (a *App) recordHubAck(hubID, key string) {
	store := a.snapshot().HubStore
	if store == nil {
		return
	}
	m := hub.Message{Kind: "agent", Event: key, Text: key}
	id, ok := parseHubID(hubID)
	if !ok {
		return
	}
	for _, u := range store.List() {
		if u.ID == id {
			m.ChatID = u.ChatID
			m.Tool = u.Tool
			m.Workspace = u.Workspace
			break
		}
	}
	_ = store.Append(m)
	a.Emit("hub:changed")
}

// onHubDelivered 是 chat.Manager 的投递回调：带 hubMsgID 的排队消息真正发起
// conn.Prompt 前触发——清掉 queued 状态并补一条「已送达」ack。
func (a *App) onHubDelivered(hubID string) {
	if hubID == "" {
		return
	}
	if store := a.snapshot().HubStore; store != nil {
		if id, ok := parseHubID(hubID); ok {
			store.SetStatus(id, "")
		}
	}
	a.recordHubAck(hubID, "ui.hub.ack_delivered")
}

// RecallHubMessage 撤回一条排队中的用户消息：从 chat 队列真移除（agent 收不到），
// 并把原消息标记为已撤回（不删除）。已投递/不存在/已归档都报错。
func (a *App) RecallHubMessage(hubMsgID int64) error {
	store := a.snapshot().HubStore
	if store == nil {
		return errNotReady
	}
	var target *hub.Message
	live := store.List()
	for i := range live {
		if live[i].ID == hubMsgID {
			target = &live[i]
			break
		}
	}
	if target == nil || target.Kind != "user" {
		return errHubNotFound
	}
	if target.Status != hub.StatusQueued {
		return errHubNotQueued
	}
	// 队列真移除：会话已关闭视为已投递（无法撤回）。
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	if !m.RecallQueued(target.ChatID, itoaHubID(hubMsgID)) {
		// 投递竞态：队列里已不在，修正状态为已送达后报错。
		store.SetStatus(hubMsgID, "")
		return errHubNotQueued
	}
	if !store.SetStatus(hubMsgID, hub.StatusRecalled) {
		return errHubNotFound
	}
	a.Emit("hub:changed")
	return nil
}

// parseHubID 把字符串形式的 hub 消息 ID 解析回数值。
func parseHubID(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n := int64(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// itoaHubID 数值转字符串（strconv 的别名，独立小函数便于测试桩内复用）。
func itoaHubID(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
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
