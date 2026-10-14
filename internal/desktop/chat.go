package desktop

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// newChatManager 用真实后端装配聊天管理器（initRealDeps 用）。
func newChatManager(a *App) *chat.Manager {
	return newChatManagerWith(a.Emit, chat.RealBackend{}, func() bool {
		return a.snapshot().Config.PermissionMode == config.PermissionModeBypass
	}, a.notifyDedupAllow, a.recordHubAgentMessage)
}

// newChatManagerWith 装配聊天管理器：时间线/权限/退出经 emit 转发给前端；
// autoAllow 非 nil 且返回真时，权限请求自动放行（不弹窗）。
// dedupe 为 chat 通知的放行判定（nil 表示不去重，测试用）：chat 通知不经
// handleAgentNotify，这里显式接入同一套去重器，防止同一条通知重复弹出。
// onAgent 非 nil 时在同一条通知放行后落 AgentHub 历史（nil 表示不记，测试用）。
// ACP 聊天不经过 hook 通道，turn 结束/出错/等权限在进程内已知，这里顺带
// 转成 notify:agent 通知（与 hook 通道同一 payload 结构、同一条气泡路径）。
func newChatManagerWith(emit func(name string, data ...any), b chat.Backend, autoAllow func() bool, dedupe func(agenthook.Payload) bool, onAgent func(agenthook.Payload)) *chat.Manager {
	var m *chat.Manager
	m = chat.NewManager(b,
		func(id string, u chat.Update) {
			emit("chat:update", map[string]any{"id": id, "update": u})
			switch u.Type {
			case "turn_done":
				emitChatNotify(emit, m, id, turnEvent(u.StopReason), "", dedupe, onAgent)
			case "error":
				emitChatNotify(emit, m, id, "error", u.Text, dedupe, onAgent)
			}
		},
		func(id string, r chat.PermissionRequest) {
			emit("chat:permission", map[string]any{"id": id, "request": r})
			emitChatNotify(emit, m, id, "Notification", r.ToolCall.Title, dedupe, onAgent)
		},
		func(id string, code int, errMsg string) {
			emit("chat:exit", map[string]any{"id": id, "exitCode": code, "error": errMsg})
		})
	m.SetAutoAllowPermission(autoAllow)
	// 技能列表不走 chat:update 时间线：单独推 chat:commands，前端存 store 供输入框补全。
	m.SetOnCommands(func(id string, cmds []chat.Command) {
		emit("chat:commands", map[string]any{"id": id, "commands": cmds})
	})
	return m
}

// turnEvent 把 stopReason 归纳为通知事件 id：只有 end_turn/空 才算完成；
// cancelled 是用户主动取消（中性，不报错也不报完成）；其余
//（max_tokens / max_turn_requests / refusal / 未知值）是 agent 提前停止——
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
// done 事件携带最后一轮 assistant 回复正文（放入 Summary），供汇总窗直接展示；
// allow 非 nil 且返回 false（窗口内重复）时同样跳过。注意被吞的只是通知气泡：
// 权限请求本身仍经 chat:permission 推给前端弹窗，不影响交互。
// onAgent 非 nil 时在放行后落 AgentHub 历史（与 hook 通道共用同一写入路径）。
// 回调总在 Manager 锁外执行，Get 取锁安全。
func emitChatNotify(emit func(name string, data ...any), m *chat.Manager, id, event, summary string, allow func(agenthook.Payload) bool, onAgent func(agenthook.Payload)) {
	if m == nil {
		return
	}
	info, ok := m.Get(id)
	if !ok || info.Key == "" {
		return
	}
	if event == "done" && summary == "" {
		summary = lastAssistantText(m.History(id))
	}
	p := agenthook.Payload{
		Tool:      info.ToolID,
		Event:     event,
		TermKey:   info.Key,
		Workspace: info.Workspace,
		Summary:   summary,
		Ts:        time.Now().UnixNano(),
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
				return OpenedSession{Kind: "chat", Chat: &info}, nil
			}
			if allowFallback {
				return a.fallbackTerminalWorkspace(wsID, toolID, err.Error())
			}
			return OpenedSession{}, err
		} else if !allowFallback {
			return OpenedSession{}, err
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
	return m.Open(key, info, chat.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, sessionID)
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
func (a *App) SendChatPrompt(id, text string) error {
	a.ensureSessionHooks()
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	if err := m.Prompt(id, text); err != nil {
		return err
	}
	a.recordHubUserMessage(id, text)
	return nil
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
func (a *App) SendChatPromptBlocks(id string, blocks []ChatBlock) error {
	a.ensureSessionHooks()
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	if len(blocks) == 0 {
		return nil
	}
	if err := m.PromptBlocks(id, chatBlocksToACP(blocks)); err != nil {
		return err
	}
	a.recordHubUserMessage(id, hubTextFromBlocks(blocks))
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

// ListChatModels 返回会话的可用模型视图；agent 未报告模型信息时返回 ok=false 形态（空视图）。
func (a *App) ListChatModels(id string) (chat.ModelsView, error) {
	m := a.chats()
	if m == nil {
		return chat.ModelsView{}, errNotReady
	}
	v, ok := m.Models(id)
	if !ok {
		return chat.ModelsView{}, nil
	}
	return v, nil
}

// SetChatModel 运行时切换会话模型。
func (a *App) SetChatModel(id, modelID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.SetModel(id, modelID)
}

// ListChatCommands 返回会话最近的技能/命令列表（available_commands_update）。
func (a *App) ListChatCommands(id string) []chat.Command {
	m := a.chats()
	if m == nil {
		return []chat.Command{}
	}
	cmds := m.Commands(id)
	if cmds == nil {
		return []chat.Command{}
	}
	return cmds
}

// CancelChat 取消指定聊天会话中正在进行的轮次。
func (a *App) CancelChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.Cancel(id)
}

// RespondChatPermission 以选中选项回答一次权限请求。
func (a *App) RespondChatPermission(id, requestID, optionID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.RespondPermission(id, requestID, optionID)
}

// CancelChatPermission 取消一次待决的权限请求（agent 侧收到 cancelled）。
func (a *App) CancelChatPermission(id, requestID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.CancelPermission(id, requestID)
}

// CloseChat 关闭聊天会话（幂等），前端关闭聊天页签时调用。
func (a *App) CloseChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.Close(id)
}

// ListChats 返回全部聊天会话快照，供前端还原页签。
func (a *App) ListChats() []chat.Info {
	m := a.chats()
	if m == nil {
		return []chat.Info{}
	}
	return m.List()
}

// ChatHistory 返回指定聊天的完整时间线（已退出的会话仍可读）。
func (a *App) ChatHistory(id string) []chat.Update {
	m := a.chats()
	if m == nil {
		return []chat.Update{}
	}
	h := m.History(id)
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
