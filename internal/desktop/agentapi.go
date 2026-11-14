package desktop

import (
	"strconv"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// agentapi.go 装配 agent 工具 API（internal/agentapi）：agentBackend 复用
// wxExecutor 的合成逻辑（同一调用面，不复制业务），Feed 经 chat.Manager 的
// 旁路 tap 转发。harness 平台 / 后续 CLI-MCP 接入层从这里取 API。

// agentBackend 把 App 既有方法适配成 agentapi.Backend。
type agentBackend struct {
	a    *App
	w    *wxExecutor
	feed *agentapi.Feed
}

// 编译期保证适配器覆盖完整端口。
var _ agentapi.Backend = (*agentBackend)(nil)

// refToSession 把 hub.SessionRef 归一成 agentapi.Session。
func refToSession(r hub.SessionRef, st agentapi.Status, summary string, ts int64) agentapi.Session {
	return agentapi.Session{
		ID:             r.ChatID,
		SessionID:      r.SessionID,
		Tool:           r.Tool,
		ToolName:       r.ToolName,
		Title:          r.Title,
		Workspace:      r.Workspace,
		Status:         st,
		TerminalActive: r.TerminalActive,
		LastTs:         ts,
		Summary:        summary,
	}
}

// ActiveSessions 返回活跃 chat 会话（SessionStatuses 已合成状态/摘要/LastTs）。
func (b *agentBackend) ActiveSessions() []agentapi.Session {
	statuses := b.w.SessionStatuses()
	out := make([]agentapi.Session, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, refToSession(st.Ref, agentapi.Status(st.State), st.Summary, st.LastTs))
	}
	return out
}

// DiskSessions 返回未激活的磁盘会话：全量合成（已做 ACP 能力过滤与终端占用
// 标记）后只留 chatID 为空的磁盘引用。
func (b *agentBackend) DiskSessions() []agentapi.Session {
	var out []agentapi.Session
	for _, r := range b.w.sessionRefs(b.w.collectAllSessions()) {
		if r.ChatID != "" {
			continue
		}
		out = append(out, refToSession(r, agentapi.StatusDisk, "", 0))
	}
	return out
}

// ArchivedIDs 返回归档名单。
func (b *agentBackend) ArchivedIDs() []string {
	st := b.a.archives()
	if st == nil {
		return nil
	}
	return st.IDs()
}

// SessionMessages 返回该 chat 的全部可见消息（滤噪、滤已撤回，时间正序）。
func (b *agentBackend) SessionMessages(chatID string) []agentapi.Message {
	store := b.a.snapshot().HubStore
	if store == nil {
		return nil
	}
	var out []agentapi.Message
	for _, m := range store.List() {
		if m.ChatID != chatID || hub.IsHubNoise(m) || m.Status == hub.StatusRecalled {
			continue
		}
		out = append(out, agentapi.Message{
			ID:        m.ID,
			SessionID: m.ChatID,
			Kind:      m.Kind,
			Text:      m.Text,
			Event:     m.Event,
			Status:    m.Status,
			Ts:        m.Ts,
		})
	}
	return out
}

func (b *agentBackend) Send(chatID, text string) error { return b.a.SendChatPrompt(chatID, text) }
func (b *agentBackend) Stop(chatID string) error       { return b.a.CancelChat(chatID) }
func (b *agentBackend) Recall(msgID int64) error       { return b.a.RecallHubMessage(msgID) }

// ListModels 返回会话可选模型与当前模型。
func (b *agentBackend) ListModels(chatID string) ([]agentapi.ModelOption, string, error) {
	v, err := b.a.ListChatModels(chatID)
	if err != nil {
		return nil, "", err
	}
	out := make([]agentapi.ModelOption, 0, len(v.Models))
	for _, m := range v.Models {
		out = append(out, agentapi.ModelOption{ID: m.ID, Name: m.Name})
	}
	return out, v.Current, nil
}

func (b *agentBackend) SetModel(chatID, model string) error { return b.a.SetChatModel(chatID, model) }

// GrantPermission 处理权限请求：与 wxExecutor 同一优先序（单次允许/单次拒绝）。
func (b *agentBackend) GrantPermission(chatID, reqID string, allow bool) error {
	if allow {
		return b.a.RespondChatPermission(chatID, reqID, "allow_once")
	}
	return b.a.RespondChatPermission(chatID, reqID, "reject_once")
}

// Create 新建 ACP 会话（走 wxExecutor.CreateNew，含 hub:busy 与模型对齐）。
func (b *agentBackend) Create(toolID, wsID, model string) (agentapi.Session, error) {
	r, err := b.w.CreateNew(toolID, wsID, model)
	if err != nil {
		return agentapi.Session{}, err
	}
	return refToSession(r, agentapi.StatusRunning, "", 0), nil
}

// Open 激活磁盘会话（走 wxExecutor.OpenSession，含能力校验与 hub:busy）。
func (b *agentBackend) Open(sessionID string) (agentapi.Session, error) {
	r, err := b.w.OpenSession(sessionID)
	if err != nil {
		return agentapi.Session{}, err
	}
	return refToSession(r, agentapi.StatusRunning, "", 0), nil
}

// Tools 返回已安装工具候选。
func (b *agentBackend) Tools() []agentapi.Option {
	var out []agentapi.Option
	for _, t := range b.a.GetTools() {
		if !t.Installed {
			continue
		}
		label := t.ID
		if t.Name != "" {
			label = t.Name
		}
		out = append(out, agentapi.Option{ID: t.ID, Label: label})
	}
	return out
}

// Workspaces 返回工作区候选。
func (b *agentBackend) Workspaces() []agentapi.Option {
	var out []agentapi.Option
	for _, ws := range b.a.GetWorkspaces() {
		out = append(out, agentapi.Option{ID: ws.Path, Label: wxLastBase(ws.Path)})
	}
	return out
}

func (b *agentBackend) Feed() *agentapi.Feed { return b.feed }

// agentFeedSource 懒创建 Feed（newChatManager 装配期与 AgentAPI 首次访问共用）。
func (a *App) agentFeedSource() *agentapi.Feed {
	a.agentFeedOnce.Do(func() { a.agentFeed = agentapi.NewFeed() })
	return a.agentFeed
}

// wireAgentFeed 把 chat.Manager 的旁路 tap 接到 agentapi.Feed。SessionID 经
// originalChatID 翻回链头空间，与 hub 落盘 ChatID / ListChats 同口径。
func (a *App) wireAgentFeed(m *chat.Manager) {
	feed := a.agentFeedSource()
	ts := func() int64 { return time.Now().UnixNano() }
	// publish 前先翻链头 ID；翻译结果为空（无归因）的事件直接丢弃——
	// 空键事件订阅者永远匹配不到，只会在 pending 权限快照里堆积。
	publish := func(e agentapi.Event) {
		if id := a.originalChatID(e.SessionID); id != "" {
			e.SessionID = id
			feed.Publish(e)
		}
	}
	m.SetOnUpdateTap(func(id string, u chat.Update) {
		publish(agentapi.Event{SessionID: id, Kind: agentapi.EventUpdate, Type: u.Type, Text: u.Text, Ts: ts()})
	})
	m.SetOnPermissionTap(func(id string, r chat.PermissionRequest) {
		publish(agentapi.Event{SessionID: id, Kind: agentapi.EventPermission, Type: r.RequestID, Text: r.ToolCall.Title, Ts: ts()})
	})
	m.SetOnExitTap(func(id string, code int, errMsg string) {
		publish(agentapi.Event{SessionID: id, Kind: agentapi.EventExit, Type: strconv.Itoa(code), Text: errMsg, Ts: ts()})
	})
}

// AgentAPI 返回 agent 工具 API（懒构造）。LLM 配置每次访问实时刷新，与微信
// 桥路由「实时读配置」同口径；未配置自主模型时 CreateNL/RouteDecide 返回
// ErrLLMNotReady。
func (a *App) AgentAPI() *agentapi.API {
	feed := a.agentFeedSource()
	a.mu.Lock()
	api := a.agentAPIV
	if api == nil {
		api = agentapi.New(&agentBackend{a: a, w: newWXExecutor(a), feed: feed})
		a.agentAPIV = api
	}
	a.mu.Unlock()
	hc, _ := hubtools.FromModelConfig(a.snapshot().Config.Model)
	api.UpdateLLM(hc)
	return api
}
