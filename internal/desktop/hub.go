package desktop

import (
	"errors"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
)

// Hub 撤回与 #f 直投相关错误（wire key，前端 translateBackend 翻译）。
var (
	errHubNotFound    = errors.New("err.hub.not_found")
	errHubNotQueued   = errors.New("err.hub.not_queued")
	errHubFollowEmpty = errors.New("err.hub.follow_empty")
	errHubFollowNone  = errors.New("err.hub.follow_none")
)

// SaveHubDefaultCreate 持久化「默认 agent 工具 + 模型」到 config.yaml（前端
// localStorage 之外的跨端副本）：微信桥 #en 的 EasyNew 读这里，浏览器侧
// localStorage 只在桌面端生效。modelID 空 = 用该工具默认模型。
func (a *App) SaveHubDefaultCreate(toolID, modelID string) error {
	return a.saveConfig(func(c *config.Config) {
		c.AgentHub.DefaultTool = toolID
		c.AgentHub.DefaultModel = modelID
	})
}

// SaveHubRouteMode 持久化 AgentHub 输入路由模式（auto | agent | force_auto）
// 到 config.yaml：前端 localStorage 之外的跨端副本，工具化路由层动态读这里。
// 非法值报 wire key（前端 translateBackend 翻译）。
func (a *App) SaveHubRouteMode(mode string) error {
	switch mode {
	case config.RouteModeAuto, config.RouteModeAgent, config.RouteModeForceAuto:
	default:
		return errors.New("err.hub.invalid_route_mode")
	}
	return a.saveConfig(func(c *config.Config) {
		c.AgentHub.RouteMode = mode
	})
}

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
	if r := a.wxRuntime(); r != nil { // 微信桥出站镜像（异步，不阻塞写入路径）
		r.OnHubMessage(m)
	}
}

// recordHubUserMessage 记录一条用户发出的消息：从 chat manager 反查归因字段。
// chatID 不存在（会话已关闭）时仍记录文本，只是 workspace/tool 缺失。
func (a *App) recordHubUserMessage(chatID, text string) {
	a.recordHubUserMessageStatus(chatID, text, "", "")
}

// recordHubUserMessageStatus 同上，但带投递状态（queued / 空）；hubID 非空时用预分配 ID。
// ChatID 统一翻回原始 ID（别名链头）：前端 hub 归因与撤回都按落盘 ID 查，
// 重建后新 ID 必须回到前端认识的旧 ID 空间（与出站事件同一口径）；
// 归因反查仍用最新 ID（manager 只认它），发消息入口已解析过，通常本就是最新。
func (a *App) recordHubUserMessageStatus(chatID, text, hubID, status string) {
	if text == "" {
		return
	}
	m := hub.Message{Kind: "user", ChatID: a.originalChatID(chatID), Text: text, Status: status}
	if mgr := a.chats(); mgr != nil {
		if info, ok := mgr.Get(chatID); ok {
			m.Tool = info.ToolID
			m.Workspace = info.Workspace
			m.SessionTitle = info.Title
			// 磁盘会话 ID 快照：会话关闭后「继续」按钮靠它精确 resume
			m.SessionID = info.SessionID
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
		if id, err := strconv.ParseInt(hubID, 10, 64); err == nil {
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
	id, err := strconv.ParseInt(hubID, 10, 64)
	if err != nil {
		return
	}
	m := hub.Message{Kind: "agent", Event: key, Text: key}
	for _, u := range store.List() {
		if u.ID == id {
			m.ChatID = u.ChatID
			m.Tool = u.Tool
			m.Workspace = u.Workspace
			m.SessionTitle = u.SessionTitle
			break
		}
	}
	_ = store.Append(m)
	a.Emit("hub:changed")
}

// onHubQueued 是 chat.Manager 的竞态兜底回调：deliverTurn 期间会话转忙改入队时触发。
// 发送链路落盘时统一先写 queued 状态（保证 ack 归因可反查），这里只需补 ack 卡片。
func (a *App) onHubQueued(hubID string) {
	if hubID == "" {
		return
	}
	a.recordHubAck(hubID, "ui.hub.ack_queued")
}

// onHubDelivered 是 chat.Manager 的投递回调：带 hubMsgID 的排队消息真正发起
// conn.Prompt 前触发——清掉 queued 状态并补一条「已送达」ack。
func (a *App) onHubDelivered(hubID string) {
	if hubID == "" {
		return
	}
	if store := a.snapshot().HubStore; store != nil {
		if id, err := strconv.ParseInt(hubID, 10, 64); err == nil {
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
	var target hub.Message
	found := false
	for _, u := range store.List() {
		if u.ID == hubMsgID {
			target = u
			found = true
			break
		}
	}
	if !found || target.Kind != "user" {
		return errHubNotFound
	}
	if target.Status != hub.StatusQueued {
		return errHubNotQueued
	}
	// 队列真移除：会话已关闭视为已投递（无法撤回）。
	// 落盘的 ChatID 是原始 ID（前端空间），manager 只认最新 ID，先经别名解析。
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(target.ChatID)
	if !m.RecallQueued(resolved, strconv.FormatInt(hubMsgID, 10)) {
		// 投递竞态：队列里已不在，修正状态为已送达后报错。
		store.SetStatus(hubMsgID, "")
		return errHubNotQueued
	}
	// RecallStatus 只允许 queued → recalled 单向迁移：并发的两次撤回里，
	// 慢的那次已越过上面检查后，不会把先到的 recalled 覆写回其他状态。
	if !store.RecallStatus(hubMsgID) {
		return errHubNotQueued
	}
	a.Emit("hub:changed")
	return nil
}

// hubEventTextKey 把 agent 事件的空正文回退成稳定 wire key（前端 translateBackend
// 渲染成当前语言）。文案是 AgentHub 里 agent 自己对客户说话，故用 ui.hub.event_*
// 第一人称 key，与通知气泡共用的 ui.notify.*（第三人称标题）分开。
// 语义映射仍与 frontend/src/lib/agentNotify.ts 的 eventLabel 保持一致：
// 一侧改动必须同步另一侧。hook 通道的 Stop 载荷没有 message 字段，不回退会在
// AgentHub 留下无正文气泡（2026-10-16 用户反馈）。
func hubEventTextKey(event string) string {
	switch event {
	case "Notification", "attention":
		return "ui.hub.event_waiting_confirm"
	case "error":
		return "ui.hub.event_error"
	case "stopped":
		return "ui.hub.event_stopped"
	case "cancelled":
		return "ui.hub.event_cancelled"
	default:
		return "ui.hub.event_done"
	}
}

// recordHubAgentMessage 记录一条 agent 回复通知（hook/OSC 与 ACP 两条通道共用）。
// 会话标题按 termKey/chatID 反查后冗余落盘（SessionTitle）：会话关闭或重启后
// 历史气泡仍能显示会话名而不是 #<id> 编码。反查顺序：
//  1. chatID → chat manager（ACP 会话）；
//  2. termKey → chat manager（ACP 会话的 Key）；
//  3. termKey → 终端管理器（内嵌终端恢复的历史会话走 hook 通道，
//     只存在于 terminal.Manager，OpenSessionTerminal 打开时已把磁盘会话
//     标题写进 terminal.Info.Title）。
// 正文为空（hook 通道 Stop 载荷没有 message 字段）时回退事件语义 wire key。
func (a *App) recordHubAgentMessage(p agenthook.Payload) {
	text := p.Summary
	if strings.TrimSpace(text) == "" {
		text = hubEventTextKey(p.Event)
	}
	m := hub.Message{
		Kind:      "agent",
		Tool:      p.Tool,
		TermKey:   p.TermKey,
		Workspace: p.Workspace,
		Text:      text,
		Event:     p.Event,
		Ts:        p.Ts,
	}
	if mgr := a.chats(); mgr != nil {
		if p.ChatID != "" {
			// 入站 ChatID 可能是链头（出站翻译后的原始 ID，已不在 manager）：
			// 先按别名链解析到最新再反查；解析不到仍按原值试一次。
			// 落盘 ChatID 统一翻回链头（与 user 消息同一口径）：前端 hub 归因、
			// 撤回、状态过滤都按落盘 ID 索引，混入 manager 侧新 ID 会让重建后的
			// agent 回复归因到一条前端不认识的聊天上。
			resolved, ok := a.resolveChatID(p.ChatID)
			if !ok {
				resolved = p.ChatID
			}
			if info, ok := mgr.Get(resolved); ok {
				m.ChatID = a.originalChatID(info.ID)
				m.SessionTitle = info.Title
				m.SessionID = info.SessionID
			}
		}
		if m.SessionTitle == "" && p.TermKey != "" {
			for _, info := range mgr.List() {
				if info.Key == p.TermKey {
					m.SessionTitle = info.Title
					if m.ChatID == "" {
						m.ChatID = a.originalChatID(info.ID) // 同口径：链头
					}
					if m.SessionID == "" {
						m.SessionID = info.SessionID
					}
					break
				}
			}
		}
	}
	// 终端 hook 通道（termKey = session:<磁盘会话ID>）不在 chat manager 里，
	// 从终端管理器反查标题补快照；OpenSessionTerminal 打开时已把磁盘会话标题
	// 写进 terminal.Info.Title。不设 ChatID：终端会话没有 chat ID，避免归因串线。
	if m.SessionTitle == "" && p.TermKey != "" {
		if tm := a.terminals(); tm != nil {
			for _, info := range tm.List() {
				if info.Key == p.TermKey {
					m.SessionTitle = info.Title
					break
				}
			}
		}
	}
	// SessionID 仍为空时从 termKey 派生：session: 前缀即磁盘会话 ID（终端 hook
	// 通道与查不到的 ACP 会话都走这里），快照供「继续」按钮精确 resume；
	// 无前缀（如新建聊天的 new:N）不设。
	if m.SessionID == "" && strings.HasPrefix(p.TermKey, "session:") {
		m.SessionID = strings.TrimPrefix(p.TermKey, "session:")
	}
	a.recordHubMessage(m)
}

// recordHubWelcome 在工作区新建 agent 会话成功后，模拟该 agent 向 hub 消息流发一条欢迎词：
// 正文是稳定 wire key+工具名插值（hub.welcome|<工具名>），自然语言由前端 translateBackend 渲染。
// 归因字段与该会话一致，前端 lastHubAgentId 回落即可命中本条、把后续消息投给该 agent。
// 不真实跑一轮（不调 Prompt）。工具名取不到时缺省插值段，前端 key 文案兜底为通用欢迎词。
func (a *App) recordHubWelcome(info chat.Info) {
	text := "hub.welcome"
	if name := a.toolDisplayName(info.ToolID); name != "" {
		text += "|" + name
	}
	a.recordHubMessage(hub.Message{
		Kind:         "agent",
		ChatID:       a.originalChatID(info.ID), // 欢迎词同样落链头（info 通常刚打开，恒等）
		Tool:         info.ToolID,
		TermKey:      info.Key,
		Workspace:    info.Workspace,
		SessionTitle: info.Title,
		SessionID:    info.SessionID,
		Text:         text,
	})
}

// toolDisplayName 按工具 ID 查扫描结果里的展示名；查不到返回空串。
func (a *App) toolDisplayName(toolID string) string {
	if toolID == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tools {
		if t.ID == toolID {
			return t.Name
		}
	}
	return ""
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
