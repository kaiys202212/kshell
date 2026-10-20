package desktop

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// newChatManager 用真实后端装配聊天管理器（initRealDeps 用）。
// Hub 投递回调在此接线：排队消息真正投递时改状态并补 ack 卡片。
// 各回调的出站事件经 emitChatOut 统一翻译：manager 侧新 ID 翻回前端持有的
// 原始 ID，重建后前端按旧 ID 索引的时间线/权限/退出状态才能继续收到。
func newChatManager(a *App) *chat.Manager {
	m := newChatManagerWith(a.emitChatOut, chat.RealBackend{}, func() bool {
		return a.snapshot().Config.PermissionMode == config.PermissionModeBypass
	}, a.notifyDedupAllow, a.recordHubAgentMessage, a.originalChatID,
		func(chatID, reqID, desc string) { // 微信桥权限菜单联动
			if r := a.wxRuntime(); r != nil && r.HasBridge() {
				r.GetBridge().OpenPermissionMenu(chatID, reqID, desc)
			}
		})
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)
	// Open 完成且 agent 报告模型后补推一次模型视图：握手期前端拉 ListChatModels
	// 可能拿空（starting 状态竞态）。emitChatOut 自带 origID 翻译，与
	// chat:commands 等出站事件同一口径。
	m.SetOnModels(func(id string) {
		v, err := a.modelsView(id)
		if err != nil {
			return
		}
		a.emitChatOut("chat:models", map[string]any{"id": id, "models": v})
	})
	return m
}

// newChatManagerWith 装配聊天管理器：时间线/权限/退出经 emit 转发给前端；
// autoAllow 非 nil 且返回真时，权限请求自动放行（不弹窗）。
// dedupe 为 chat 通知的放行判定（nil 表示不去重，测试用）：chat 通知不经
// handleAgentNotify，这里显式接入同一套去重器，防止同一条通知重复弹出。
// onAgent 非 nil 时在同一条通知放行后落 AgentHub 历史（nil 表示不记，测试用）。
// ACP 聊天不经过 hook 通道，turn 结束/出错/等权限在进程内已知，这里顺带
// 转成 notify:agent 通知（与 hook 通道同一 payload 结构、同一条气泡路径）。
// origID 把 manager 侧会话 ID 翻回前端持有的原始 ID（nil 为恒等，测试用）：
// 重建换 ID 后前端仍按旧 ID 索引，出站事件必须统一翻译（见 emitChatOut）。
// onWxPermission 非 nil 时权限请求同步转发给微信桥的命令层权限菜单
// （nil 安全：测试装配不传；desktop 装配传闭包，运行时内部再判桥是否存在）。
func newChatManagerWith(emit func(name string, data ...any), b chat.Backend, autoAllow func() bool, dedupe func(agenthook.Payload) bool, onAgent func(agenthook.Payload), origID func(string) string, onWxPermission func(chatID, reqID, desc string)) *chat.Manager {
	translate := func(id string) string {
		if origID == nil {
			return id
		}
		return origID(id)
	}
	var m *chat.Manager
	m = chat.NewManager(b,
		func(id string, u chat.Update) {
			emit("chat:update", map[string]any{"id": translate(id), "update": u})
			switch u.Type {
			case "turn_done":
				emitChatNotify(emit, m, id, turnEvent(u.StopReason), stopFallback(u.StopReason), dedupe, onAgent, origID)
			case "error":
				emitChatNotify(emit, m, id, "error", u.Text, dedupe, onAgent, origID)
			}
		},
		func(id string, r chat.PermissionRequest) {
			emit("chat:permission", map[string]any{"id": translate(id), "request": r})
			emitChatNotify(emit, m, id, "Notification", r.ToolCall.Title, dedupe, onAgent, origID)
			if onWxPermission != nil { // 微信桥权限菜单联动（nil 安全，测试装配不传）
				onWxPermission(translate(id), r.RequestID, r.ToolCall.Title)
			}
		},
		func(id string, code int, errMsg string) {
			emit("chat:exit", map[string]any{"id": translate(id), "exitCode": code, "error": errMsg})
		})
	m.SetAutoAllowPermission(autoAllow)
	// 技能列表不走 chat:update 时间线：单独推 chat:commands，前端存 store 供输入框补全。
	m.SetOnCommands(func(id string, cmds []chat.Command) {
		emit("chat:commands", map[string]any{"id": translate(id), "commands": cmds})
	})
	// 待发送队列同理：队列变化时推全量快照，前端镜像后展示/撤销。
	// onQueue 总在 manager 解锁后触发，emit 不会阻塞聊天主路径。
	m.SetOnQueue(func(id string, items []chat.QueueItem) {
		emit("chat:queue", map[string]any{"id": translate(id), "items": items})
	})
	return m
}

// turnEvent 把 stopReason 归纳为通知事件 id：只有 end_turn/空 才算完成；
// cancelled 是用户主动取消（中性，不报错也不报完成）；其余
// （max_tokens / max_turn_requests / refusal / 未知值）是 agent 提前停止——
// 此前一律报「任务完成」，正是「任务没做完却停了且毫无提示」的根因。
func turnEvent(stop string) string {
	switch stop {
	case "", "end_turn":
		return "done"
	case "cancelled":
		return "cancelled"
	default:
		return "stopped"
	}
}

// stopFallback 把停止原因翻成前端 wire key（turn_stopped_reason|<原因>），
// 作为 stopped 事件 assistant 正文为空时的兜底文案：气泡至少说明「为什么停」，
// 而不是一条空卡片。done/cancelled 不需要兜底。
func stopFallback(stop string) string {
	if turnEvent(stop) != "stopped" || stop == "" {
		return ""
	}
	return "ui.notify.turn_stopped_reason|" + stop
}

// lastAssistantText 取会话时间线中最后一轮 assistant 回复的拼接文本。
// 「最后一轮」= 最后一条 assistant 的 MessageID；同一 MessageID 的分片按序拼接。
// 返回空串表示尚无回复；超长按 rune 截断避免 payload 过大（maxLen 为字符数，
// 按字节切会把中文字符切成非法 UTF-8，JSON 里变 U+FFFD 乱码）。
func lastAssistantText(h []chat.Update) string {
	const maxLen = 2000
	lastID := ""
	idx := -1
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Type == "assistant" {
			lastID = h[i].MessageID
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	var b strings.Builder
	for _, u := range h {
		if u.Type == "assistant" && u.MessageID == lastID {
			b.WriteString(u.Text)
		}
	}
	s := strings.TrimSpace(b.String())
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen])
	}
	return s
}

// emitChatNotify 把一条聊天侧事件转成 agent 通知气泡 payload。
// 归因字段（key/tool/workspace）从 manager 反查；会话已关闭时静默跳过；
// done 与 stopped 事件携带最后一轮 assistant 回复正文（放入 Summary），供汇总窗
// 直接展示——stopped（max_tokens/refusal 等提前停止）此前不回填，AgentHub 落
// 空气泡（正文只有事件语义，用户看不到 agent 实际说了什么）；正文仍为空时
// 回退停止原因 wire key，保证气泡至少说明「为什么停」。
// allow 非 nil 且返回 false（窗口内重复）时同样跳过。注意被吞的只是通知气泡：
// 权限请求本身仍经 chat:permission 推给前端弹窗，不影响交互。
// onAgent 非 nil 时在放行后落 AgentHub 历史（与 hook 通道共用同一写入路径）。
// 回调总在 Manager 锁外执行，Get 取锁安全。
// chatID 翻回原始 ID（前端归因空间），与出站事件同一口径；origID 为 nil 时恒等
// （测试装配不关心别名链）。
func emitChatNotify(emit func(name string, data ...any), m *chat.Manager, id, event, summary string, allow func(agenthook.Payload) bool, onAgent func(agenthook.Payload), origID func(string) string) {
	if m == nil {
		return
	}
	info, ok := m.Get(id)
	if !ok || info.Key == "" {
		return
	}
	if (event == "done" || event == "stopped") && summary == "" {
		summary = lastAssistantText(m.History(id))
	}
	if summary == "" && event == "stopped" {
		summary = "ui.notify.turn_stopped_reason"
	}
	chatID := info.ID
	if origID != nil {
		chatID = origID(info.ID)
	}
	p := agenthook.Payload{
		Tool:      info.ToolID,
		Event:     event,
		TermKey:   info.Key,
		Workspace: info.Workspace,
		Summary:   summary,
		Ts:        time.Now().UnixNano(),
		ChatID:    chatID,
	}
	if allow != nil && !allow(p) {
		return
	}
	emit("notify:agent", p)
	if onAgent != nil {
		onAgent(p)
	}
}

func (a *App) chats() *chat.Manager { return a.snapshot().Chats }

// emitChatOut 是聊天事件的统一出站口：把 manager 给的会话 ID 翻回前端持有的
// 最原始 ID（别名链头）再推送。重建换 ID 后前端页签/hub 归因仍按旧 ID 索引，
// 不翻译的话 chat:update/permission/exit 会指向一个前端不认识的新 ID，
// 回复不可见、running 状态永远回不到 ready。新会话（从未重建过）零开销。
// payload 必须是单参数 map 且含 "id" 键（newChatManagerWith 的约定）。
func (a *App) emitChatOut(name string, data ...any) {
	if len(data) == 1 {
		if m, ok := data[0].(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				m["id"] = a.originalChatID(id)
			}
		}
	}
	a.Emit(name, data...)
}

// OpenedSession 是统一打开结果：Kind 为 "chat" 或 "terminal"。
type OpenedSession struct {
	Kind     string         `json:"Kind"`
	Chat     *chat.Info     `json:"Chat,omitempty"`
	Terminal *terminal.Info `json:"Terminal,omitempty"`
	Fallback string         `json:"Fallback,omitempty"`
}

// preferACP 是否按配置偏好走 ACP 路径（仅 session_mode=acp）。
func (a *App) preferACP() bool {
	return a.snapshot().Config.SessionMode == config.SessionModeACP
}

// OpenSession 恢复历史会话：偏好 ACP 且可用时走聊天，否则终端。
func (a *App) OpenSession(sessionID string) (OpenedSession, error) {
	if !a.preferACP() {
		return a.fallbackTerminalSession(sessionID, "")
	}
	return a.openSessionACP(sessionID, true)
}

// OpenSessionACP 显式以 ACP 打开历史会话（忽略会话模式偏好）。
func (a *App) OpenSessionACP(sessionID string) (OpenedSession, error) {
	return a.openSessionACP(sessionID, false)
}

func (a *App) openSessionACP(sessionID string, allowFallback bool) (OpenedSession, error) {
	a.ensureSessionHooks()
	s, tools, ok := a.sessionByIDReady(sessionID)
	if !ok {
		return OpenedSession{}, errSessionNotFound
	}
	o := a.snapshot()
	if m := o.Chats; m != nil {
		if l, err := launch.ForSessionACP(o.Providers, tools, s, a.modelOptions(), a.permissionOptions()); err == nil {
			info, err := openChat(m, "session:"+sessionID, chat.Info{
				Kind:      chat.KindSession,
				SessionID: s.ID,
				Workspace: s.Workspace,
				Title:     s.Title,
				ToolID:    s.ToolID,
			}, l, s.ID)
			if err == nil {
				a.emitChatMeta(info)
				return OpenedSession{Kind: "chat", Chat: &info}, nil
			}
			if allowFallback {
				return a.fallbackTerminalSession(sessionID, err.Error())
			}
			return OpenedSession{}, err
		} else if !allowFallback {
			return OpenedSession{}, err
		}
	} else if !allowFallback {
		return OpenedSession{}, errNotReady
	}
	if allowFallback {
		return a.fallbackTerminalSession(sessionID, "")
	}
	return OpenedSession{}, errNotReady
}

// OpenWorkspace 在工作区新建会话：偏好 ACP 且可用时走聊天，否则终端。
func (a *App) OpenWorkspace(wsID, toolID string) (OpenedSession, error) {
	if !a.preferACP() {
		return a.fallbackTerminalWorkspace(wsID, toolID, "")
	}
	return a.openWorkspaceACP(wsID, toolID, true)
}

// OpenWorkspaceACP 显式以 ACP 新建会话（忽略会话模式偏好）。
func (a *App) OpenWorkspaceACP(wsID, toolID string) (OpenedSession, error) {
	return a.openWorkspaceACP(wsID, toolID, false)
}

func (a *App) openWorkspaceACP(wsID, toolID string, allowFallback bool) (OpenedSession, error) {
	a.ensureSessionHooks()
	ws, tools, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return OpenedSession{}, errWorkspaceNotFound
	}
	o := a.snapshot()
	if m := o.Chats; m != nil {
		if l, err := launch.ForWorkspaceACP(o.Providers, tools, ws, toolID, a.modelOptions(), a.permissionOptions()); err == nil {
			info, err := openChat(m, "new:"+nextChatSeq(), chat.Info{
				Kind:            chat.KindNew,
				Workspace:       ws.Path,
				Title:           workspaceTerminalTitle(o.Providers, tools, ws, toolID),
				ToolID:          toolID,
				KnownSessionIDs: a.knownSessionIDs(),
			}, l, "")
			if err == nil {
				// 先让前端登记会话再落欢迎词：欢迎卡一旦可见，快捷操作就能解析到目标。
				a.emitChatMeta(info)
				a.recordHubWelcome(info)
				return OpenedSession{Kind: "chat", Chat: &info}, nil
			}
			if allowFallback {
				return a.fallbackTerminalWorkspace(wsID, toolID, err.Error())
			}
			return OpenedSession{}, err
		} else if !allowFallback {
			return OpenedSession{}, err
		} else if errors.Is(err, launch.ErrNotLoggedIn) {
			// 未登录被新建 ACP 门禁拒绝：偏好路径不算错误，静默降级终端并把
			// wire key 带进 Fallback 供前端提示「未登录已降级，去登录」；
			// 显式 ACP 路径已在上一分支原样报错。其余错误维持原行为
			//（落到下方 fallbackTerminalWorkspace，reason 仍为空）。
			return a.fallbackTerminalWorkspace(wsID, toolID, launch.ErrNotLoggedIn.Error())
		}
	} else if !allowFallback {
		return OpenedSession{}, errNotReady
	}
	if allowFallback {
		return a.fallbackTerminalWorkspace(wsID, toolID, "")
	}
	return OpenedSession{}, errNotReady
}

func openChat(m *chat.Manager, key string, info chat.Info, l providers.Launch, sessionID string) (chat.Info, error) {
	spec, err := launcher.Build(l)
	if err != nil {
		return chat.Info{}, err
	}
	return m.Open(key, info, chat.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env, ACPSettingsJSON: l.ACPSettingsJSON}, sessionID)
}

func (a *App) fallbackTerminalSession(sessionID, reason string) (OpenedSession, error) {
	info, err := a.OpenSessionTerminal(sessionID, 0, 0)
	if err != nil {
		return OpenedSession{}, err
	}
	return OpenedSession{Kind: "terminal", Terminal: &info, Fallback: reason}, nil
}

func (a *App) fallbackTerminalWorkspace(wsID, toolID, reason string) (OpenedSession, error) {
	info, err := a.OpenWorkspaceTerminal(wsID, toolID, 0, 0)
	if err != nil {
		return OpenedSession{}, err
	}
	return OpenedSession{Kind: "terminal", Terminal: &info, Fallback: reason}, nil
}

// SendChatPrompt 向指定聊天会话发起一轮用户输入。
// 发送成功后落盘 Hub 用户消息（忙时标记 queued 可撤回），并落一条模拟 agent
// 确认卡片（入队 → ack_queued；立即投递的「已送达」ack 由 onHubDelivered 补）。
// 入口先按别名链把旧 ID 解析到最新：重建过的会话前端仍持旧 ID 也能继续发送；
// 已退出时自动重建（ensureChatAlive）。
func (a *App) SendChatPrompt(id, text string) (err error) {
	// 成功投递即视为用户已转向另一条消息：AgentHub 的待选暂存正文失效，
	// 否则之后的纯数字可能把旧正文发给错误会话。失败时保留待选，便于重试。
	defer func() {
		if err == nil {
			a.clearHubPendingPick()
		}
	}()
	a.ensureSessionHooks()
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	// #f 直投指令（follow 最新 agent 会话）：先于工具化路由解析，不走意图
	// 识别，也不依赖模型配置；目标已退出时经 ensureChatAlive 按快照重建。
	if rest, ok := parseFollowPrompt(text); ok {
		if rest == "" {
			return errHubFollowEmpty
		}
		target := a.latestAgentChatID()
		if target == "" {
			return errHubFollowNone
		}
		resolved, _ := a.resolveChatID(target)
		sendID, err := a.ensureChatAlive(resolved)
		if err != nil {
			return err
		}
		blocks := []acp.ContentBlock{{Type: "text", Text: rest}}
		return a.sendAndRecord(sendID, blocks, rest)
	}
	resolved, _ := a.resolveChatID(id)
	sendID, err := a.ensureChatAlive(resolved)
	if err != nil {
		return err
	}
	blocks := []acp.ContentBlock{{Type: "text", Text: text}}
	// 工具化路由分支：仅纯文本路径参与（Blocks 附件路径直接走原链路）。
	// handled=true 时输入已被 hubtools 消费（含落盘）；false = 模型未配置 /
	// RouteMode=agent / LLM 失败降级，落回原直连链路。
	if h := a.hubTools(); h != nil {
		handled, err := h.HandlePrompt(sendID, text)
		if handled {
			return err
		}
	}
	return a.sendAndRecord(sendID, blocks, text)
}

// parseFollowPrompt 解析 #f 直投指令：输入整串为 "#f" 时返回空消息体；
// "#f <消息>" 返回消息体。其余输入不是指令（false）。
func parseFollowPrompt(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if t == "#f" {
		return "", true
	}
	if rest, ok := strings.CutPrefix(t, "#f "); ok {
		return strings.TrimSpace(rest), true
	}
	return "", false
}

// latestAgentChatID 返回 hub store 最近一条纯文本 agent 消息归属的会话 ID，
// 没有时返回空串。#f 直投的目标来源。
func (a *App) latestAgentChatID() string {
	store := a.snapshot().HubStore
	if store == nil {
		return ""
	}
	list := store.List()
	for i := len(list) - 1; i >= 0; i-- {
		if mm := list[i]; mm.Kind == "agent" && mm.Event == "" && mm.ChatID != "" && mm.Text != "" {
			return mm.ChatID
		}
	}
	return ""
}

// ChatBlock 是前端发来的多形态内容块（扁平结构）：text 用 Text；
// image 用 Data(base64)+MimeType；resource_link 用 URI+Name；
// resource（粘贴的小文件内容）用 URI+MimeType+Data。
type ChatBlock struct {
	Type     string `json:"Type"`
	Text     string `json:"Text,omitempty"`
	Data     string `json:"Data,omitempty"`
	MimeType string `json:"MimeType,omitempty"`
	URI      string `json:"URI,omitempty"`
	Name     string `json:"Name,omitempty"`
}

// chatBlockToACP 转成协议层内容块；resource 形态组装内嵌资源体。
func chatBlockToACP(b ChatBlock) acp.ContentBlock {
	out := acp.ContentBlock{Type: b.Type, Text: b.Text, Data: b.Data, MimeType: b.MimeType, URI: b.URI, Name: b.Name}
	if b.Type == "resource" {
		out.Resource = &acp.ResourceContents{URI: b.URI, MimeType: b.MimeType, Blob: b.Data, Text: b.Text}
		out.Data = "" // resource 的内容放 Resource 内，避免顶层重复序列化
		out.URI = ""
		out.Name = ""
	}
	return out
}

func chatBlocksToACP(blocks []ChatBlock) []acp.ContentBlock {
	out := make([]acp.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, chatBlockToACP(b))
	}
	return out
}

// SendChatPromptBlocks 向指定聊天发起一轮多形态内容块输入（文本 + 附件）。
// 入口别名解析与重建同 SendChatPrompt。
func (a *App) SendChatPromptBlocks(id string, blocks []ChatBlock) (err error) {
	a.ensureSessionHooks()
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	if len(blocks) == 0 {
		return nil
	}
	// 同 SendChatPrompt：附件投递成功同样让待选暂存正文失效。
	defer func() {
		if err == nil {
			a.clearHubPendingPick()
		}
	}()
	resolved, _ := a.resolveChatID(id)
	sendID, err := a.ensureChatAlive(resolved)
	if err != nil {
		return err
	}
	acpBlocks := chatBlocksToACP(blocks)
	return a.sendAndRecord(sendID, acpBlocks, hubTextFromBlocks(blocks))
}

// sendAndRecord 统一发送与 Hub 落盘：预留消息 ID → 先落 user 消息（queued 状态，
// 保证投递回调触发时消息已在 store，ack 归因能反查到）→ 再投递（忙时入队）。
// 空闲投递 / 竞态入队两条路径的状态修正与 ack 卡片都由 onHubDelivered / onHubQueued
// 回调统一处理，这里不再区分 queued 与否，避免回调先于落盘的竞态。
// hubText 为空时（纯附件）不落 Hub 消息，也不预留 ID。
func (a *App) sendAndRecord(chatID string, blocks []acp.ContentBlock, hubText string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	hubID := ""
	if store := a.snapshot().HubStore; store != nil && hubText != "" {
		hubID = strconv.FormatInt(store.ReserveID(), 10)
		a.recordHubUserMessageStatus(chatID, hubText, hubID, hub.StatusQueued)
	}
	queued, err := m.PromptBlocksHub(chatID, blocks, hubID)
	if err != nil {
		return err
	}
	if queued && hubID != "" {
		// 忙分支入队不触发 onHubQueued 回调（回调只覆盖 deliverTurn 竞态兜底），
		// 这里的 ack 由返回值直接落；竞态兜底路径的 ack 由回调补。
		a.recordHubAck(hubID, "ui.hub.ack_queued")
	}
	return nil
}

// hubTextFromBlocks 拼接内容块里的文本作为 Hub 用户消息正文（附件只记文本部分）。
func hubTextFromBlocks(blocks []ChatBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// ListChatQueue 返回会话当前待投递队列快照（输入框上方展示与撤销）。
// 入口别名解析：重建后前端持旧 ID 也能查到新会话的队列。
func (a *App) ListChatQueue(id string) []chat.QueueItem {
	m := a.chats()
	if m == nil {
		return []chat.QueueItem{}
	}
	resolved, _ := a.resolveChatID(id)
	return m.Queue(resolved)
}

// RemoveChatQueued 撤销一条待投递消息（真移除，agent 收不到）。
// Hub 来源条目同步把原消息标记为 recalled；本地条目只出队。
// 条目已投递/已被撤销时返回 err.chat.queue_gone。
func (a *App) RemoveChatQueued(id, itemID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	hubMsgID, err := m.RemoveQueued(resolved, itemID)
	if err != nil {
		return err
	}
	if hubMsgID != "" {
		a.markHubRecalled(hubMsgID)
	}
	return nil
}

// ListChatModels 返回会话的可用模型视图；实现与 chat:models 推送共用 modelsView。
func (a *App) ListChatModels(id string) (chat.ModelsView, error) {
	return a.modelsView(id)
}

// modelsView 取某会话的合并模型视图；agent 未报告时返回空切片视图
// （nil 会序列化成 null，前端 models.Models.find 会抛 TypeError 白屏）。
// ListChatModels 与 chat:models 推送共用。入口别名解析：重建后前端持旧 ID
// 也能查到新会话。agent 上报的列表会与 kshell 配置的平台模型合并（配置模型
// 是注入通道的实际取值域，agent 自报列表里不会有它们；单平台裸名、多平台
// 加前缀追加，去重，详见 mergeConfigModels）。
func (a *App) modelsView(id string) (chat.ModelsView, error) {
	m := a.chats()
	if m == nil {
		return chat.ModelsView{}, errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	v, ok := m.Models(resolved)
	if !ok {
		v = chat.ModelsView{Models: []chat.ModelView{}}
	}
	return a.mergeConfigModels(v), nil
}

// mergeConfigModels 把 kshell 配置的模型列表并入 agent 上报视图：
// 已存在（按 ID）跳过；数据源为配置的平台列表。单平台保持裸名
// （ID=Name=model，向后兼容旧保存的 modelId）；多平台加
// "kshell:<platID>:<model>" 前缀，默认平台条目排最前，其余按列表顺序。
// 不改写 Current：当前模型以 agent 上报为准（默认模型由 agent 自行决定）。
func (a *App) mergeConfigModels(v chat.ModelsView) chat.ModelsView {
	cfg := a.snapshot().Config.Model
	if !cfg.Enabled || len(cfg.Platforms) == 0 {
		return v
	}
	seen := make(map[string]bool, len(v.Models))
	for _, mo := range v.Models {
		seen[mo.ID] = true
	}
	add := func(id, name string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		v.Models = append(v.Models, chat.ModelView{ID: id, Name: name})
	}
	if len(cfg.Platforms) == 1 {
		for _, name := range cfg.Platforms[0].Models {
			name = strings.TrimSpace(name)
			add(name, name)
		}
		return v
	}
	// 多平台：默认平台排最前（命中 DefaultPlatform，否则第一个），其余按列表顺序。
	def, _ := cfg.ActivePlatform()
	var defPlat *config.ModelPlatform
	rest := make([]config.ModelPlatform, 0, len(cfg.Platforms))
	for _, p := range cfg.Platforms {
		if p.ID == def.ID && defPlat == nil {
			pc := p
			defPlat = &pc
			continue
		}
		rest = append(rest, p)
	}
	ordered := rest
	if defPlat != nil {
		ordered = append([]config.ModelPlatform{*defPlat}, rest...)
	}
	for _, p := range ordered {
		for _, name := range p.Models {
			name = strings.TrimSpace(name)
			add("kshell:"+p.ID+":"+name, p.Name+": "+name)
		}
	}
	return v
}

// splitKshellModelID 解析下拉前缀 ID "kshell:<platID>:<model>"。
// platID 是平台 ID（不含冒号），model 可能含冒号，因此去掉 "kshell:" 头后
// 按第一个冒号切分，剩余全部视为 model。非前缀或 platID 为空返回 ok=false。
func splitKshellModelID(id string) (platID, model string, ok bool) {
	rest, found := strings.CutPrefix(id, "kshell:")
	if !found {
		return "", "", false
	}
	i := strings.Index(rest, ":")
	if i <= 0 {
		return "", "", false
	}
	model = rest[i+1:]
	if model == "" {
		// 畸形 ID（如 "kshell:plat:"）：空模型名不应下发给 SetModel，按透传容错。
		return "", "", false
	}
	return rest[:i], model, true
}

// SetChatModel 运行时切换会话模型。入口别名解析同 ListChatModels。
// 下拉前缀 ID 在转发前解析：默认平台只切模型；跨平台先落盘切换默认平台
// 与活跃模型再切（保存失败则不切）；platID 已不在配置（配置删除后下拉
// 缓存未刷）时原样透传容错，不崩、不误存。
func (a *App) SetChatModel(id, modelID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	platID, model, ok := splitKshellModelID(modelID)
	if !ok {
		return m.SetModel(resolved, modelID)
	}
	cfg := a.snapshot().Config.Model
	found := false
	for _, p := range cfg.Platforms {
		if p.ID == platID {
			found = true
			break
		}
	}
	if !found {
		return m.SetModel(resolved, modelID)
	}
	if def, _ := cfg.ActivePlatform(); def.ID != platID {
		if err := a.saveConfig(func(c *config.Config) {
			c.Model.DefaultPlatform = platID
			c.Model.ActiveModel = model
		}); err != nil {
			return err
		}
	}
	return m.SetModel(resolved, model)
}

// ListChatCommands 返回会话最近的技能/命令列表（available_commands_update）。
// 入口别名解析同 ListChatModels。
func (a *App) ListChatCommands(id string) []chat.Command {
	m := a.chats()
	if m == nil {
		return []chat.Command{}
	}
	resolved, _ := a.resolveChatID(id)
	cmds := m.Commands(resolved)
	if cmds == nil {
		return []chat.Command{}
	}
	return cmds
}

// CancelChat 取消指定聊天会话中正在进行的轮次。入口别名解析同 ListChatModels。
func (a *App) CancelChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	return m.Cancel(resolved)
}

// RespondChatPermission 以选中选项回答一次权限请求。入口别名解析同 ListChatModels。
func (a *App) RespondChatPermission(id, requestID, optionID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	return m.RespondPermission(resolved, requestID, optionID)
}

// CancelChatPermission 取消一次待决的权限请求（agent 侧收到 cancelled）。
// 入口别名解析同 ListChatModels。
func (a *App) CancelChatPermission(id, requestID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	return m.CancelPermission(resolved, requestID)
}

// CloseChat 关闭聊天会话（幂等），前端关闭聊天页签时调用。
// 关闭后队列清空、排队消息不再投递，Hub store 里该会话的 queued 状态
// 必须同步失效，否则前端永远显示「排队中」而撤回必失败。
// 入口别名解析：重建后前端仍持旧 ID 也能关掉新会话。成功后整链清理
// 别名（forgetChatChain），避免已关会话被旧 ID 静默复活。
// 清 queued 用原始 ID：hub 落盘的 ChatID 已统一翻回前端 ID 空间。
func (a *App) CloseChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := a.resolveChatID(id)
	err := m.Close(resolved)
	if store := a.snapshot().HubStore; store != nil {
		store.ClearQueuedByChat(a.originalChatID(resolved))
	}
	if err == nil {
		a.forgetChatChain(resolved)
	}
	return err
}

// ListChats 返回全部聊天会话快照，供前端还原页签。
// 每条 Info.ID 统一翻回别名链头：前端镜像按 ID 整表映射（scan:done 后
// setChats(mergeMirrorList)），manager 侧新 ID 不翻译的话旧 ID 行会被当成
// 陌生行丢弃，重建过的页签直接消失。调用方（前端还原页签 / 归因反查）都以
// 旧 ID 空间为索引，无需要新 ID 的场景。
func (a *App) ListChats() []chat.Info {
	m := a.chats()
	if m == nil {
		return []chat.Info{}
	}
	list := m.List()
	for i := range list {
		list[i].ID = a.originalChatID(list[i].ID)
	}
	return list
}

// ChatHistory 返回指定聊天的完整时间线（已退出的会话仍可读）。
// 入口别名解析：重建后前端持旧 ID 也能读到新会话时间线。
func (a *App) ChatHistory(id string) []chat.Update {
	m := a.chats()
	if m == nil {
		return []chat.Update{}
	}
	resolved, _ := a.resolveChatID(id)
	h := m.History(resolved)
	if h == nil {
		return []chat.Update{}
	}
	return h
}

// nextChatSeq 给工作区新建聊天生成唯一 key（每次新建都要新进程）。
func nextChatSeq() string {
	return strconv.FormatInt(chatKeySeq.Add(1), 10)
}

var chatKeySeq atomic.Int64
