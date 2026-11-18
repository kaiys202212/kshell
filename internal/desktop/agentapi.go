package desktop

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/agentsrv"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/version"
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

// sessionMtimes 取磁盘会话 ID → 最后活跃时间：providers.Session 的 UpdatedAt
// 是解析会话记录得到的最大时间戳（收集列表 wxChatInfo 本身不带时间）。
func (b *agentBackend) sessionMtimes() map[string]int64 {
	mt := make(map[string]int64)
	for _, s := range b.a.GetSessions() {
		mt[s.ID] = s.UpdatedAt.UnixNano()
	}
	return mt
}

// DiskSessions 返回未激活的磁盘会话：全量合成（已做 ACP 能力过滤与终端占用
// 标记）后只留 chatID 为空的磁盘引用。LastTs 取扫描产物的 UpdatedAt（与
// WorkspaceSessions 同源免费），供排序与展示。
func (b *agentBackend) DiskSessions() []agentapi.Session {
	mt := b.sessionMtimes()
	var out []agentapi.Session
	for _, r := range b.w.sessionRefs(b.w.collectAllSessions()) {
		if r.ChatID != "" {
			continue
		}
		out = append(out, refToSession(r, agentapi.StatusDisk, "", mt[r.SessionID]))
	}
	return out
}

// WorkspaceSessions 返回指定工作区最近 n 个磁盘会话（按最后活跃时间新→旧）。
// 与 DiskSessions 同源（collectAllSessions + sessionRefs，已做 ACP 能力过滤
// 与终端占用标记），时间取解析记录的 UpdatedAt（个别工具解析不出时为 0，
// 排序时与有时间的条目混排不保证相对次序，故用稳定排序保证确定性输出）。
func (b *agentBackend) WorkspaceSessions(wsPath string, n int) []agentapi.Session {
	if n <= 0 {
		return nil
	}
	mt := b.sessionMtimes()
	var matched []agentapi.Session
	for _, r := range b.w.sessionRefs(b.w.collectAllSessions()) {
		if r.ChatID != "" || r.Workspace != wsPath {
			continue
		}
		matched = append(matched, refToSession(r, agentapi.StatusDisk, "", mt[r.SessionID]))
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].LastTs > matched[j].LastTs })
	if len(matched) > n {
		matched = matched[:n]
	}
	return matched
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

// RecentHubMessages 返回 AgentHub 最近 n 条可见消息（时间正序，已滤噪），
// 映射归因字段供管理者 system 注入「最近投递语境」。取数复用
// wxExecutor.RecentMessages（与 #h 命令同源，IsHubNoise 过滤一致）。
func (b *agentBackend) RecentHubMessages(n int) []agentapi.Message {
	msgs := b.w.RecentMessages(n)
	if len(msgs) == 0 {
		return nil
	}
	out := make([]agentapi.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, agentapi.Message{
			ID:           m.ID,
			SessionID:    m.ChatID,
			Kind:         m.Kind,
			Text:         m.Text,
			Event:        m.Event,
			Status:       m.Status,
			Ts:           m.Ts,
			Tool:         m.Tool,
			SessionTitle: m.SessionTitle,
			ViaHub:       m.ViaHub,
		})
	}
	return out
}

// Send 是管理者 send 工具的投递路径：经 sendChatPromptVia 标记代发，
// 目标会话的 user 消息与 hub 落盘记录均带 ViaHub（前端打「由 AgentHub 自动发送」标签）。
func (b *agentBackend) Send(chatID, text string) error { return b.a.sendChatPromptVia(chatID, text, true) }
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

// DefaultCreate 返回 AgentHub ⚙ 默认设置（toolID/modelID），供管理者循环
// create_session 在用户未点名工具/模型时填空。config.yaml 是唯一权威。
func (b *agentBackend) DefaultCreate() (string, string) { return b.a.agenthubDefaultCreate() }

// Tools 返回已安装工具候选；仅含 ACP 可用工具（create_session 的候选与校验
// 同源，非 ACP 工具被模型选中后创建必报错，提前从候选中剔除）。
func (b *agentBackend) Tools() []agentapi.Option {
	var out []agentapi.Option
	for _, t := range b.a.GetTools() {
		if !t.Installed || !launch.ACPCapable(t) {
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

// agentAPIEndpointPath 返回连接发现文件路径：Layout.Root/agentapi/endpoint.json
//（Root 缺省回落 temp，与 archives 同兜底口径）。
func (a *App) agentAPIEndpointPath() string {
	root := a.opts.Layout.Root
	if root == "" && a.opts.Home != "" {
		root = filepath.Join(a.opts.Home, ".kshell")
	}
	if root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "agentapi", "endpoint.json")
}

// startAgentSrv 启动本机 agent API 服务并写发现文件。失败仅记日志：服务缺席
// 只影响 CLI（endpoint 文件缺失 → exit 3），不能拖垮桌面端启动。
func (a *App) startAgentSrv() {
	srv, err := agentsrv.Start(agentsrv.Options{API: a.AgentAPI(), EndpointPath: a.agentAPIEndpointPath(), AppVersion: version.Current()})
	if err != nil {
		log.Printf("agentsrv 启动失败（CLI 将不可用）: %v", err)
		return
	}
	a.mu.Lock()
	a.agentSrv = srv
	a.mu.Unlock()
}

// AgentAPI 返回 agent 工具 API（懒构造）。LLM 配置每次访问实时刷新，与微信
// 桥路由「实时读配置」同口径；未配置自主模型时 CreateNL/RouteDecide 返回
// ErrLLMNotReady。构造时注入管理者记忆 journal（~/.kshell/hub/agent），
// 供管理者循环读写历史轮次。
func (a *App) AgentAPI() *agentapi.API {
	feed := a.agentFeedSource()
	a.mu.Lock()
	api := a.agentAPIV
	if api == nil {
		api = agentapi.New(&agentBackend{a: a, w: newWXExecutor(a), feed: feed})
		api.SetMemory(agentapi.NewManagerMemory(a.hubAgentMemoryDir()))
		a.agentAPIV = api
	}
	a.mu.Unlock()
	hc, _ := hubtools.FromModelConfig(a.snapshot().Config.Model)
	api.UpdateLLM(hc)
	return api
}

// hubAgentMemoryDir 返回管理者记忆 journal 目录 ~/.kshell/hub/agent：
// ~/.kshell 根的取法与 agentAPIEndpointPath 同链路（Layout.Root 优先，测试
// 用 Home 拼装），无配置来源时兜底 os.UserHomeDir。目录不存在时创建
//（NewManagerMemory.Append 内部还会兜一次，这里提前建好让 load 阶段就绪）。
func (a *App) hubAgentMemoryDir() string {
	root := a.opts.Layout.Root
	if root == "" && a.opts.Home != "" {
		root = filepath.Join(a.opts.Home, ".kshell")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		root = filepath.Join(home, ".kshell")
	}
	dir := filepath.Join(root, "hub", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("创建管理者记忆目录失败（记忆将不可用）: %v", err)
	}
	return dir
}
