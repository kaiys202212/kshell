package desktop

import (
	"strconv"
	"sync/atomic"

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
	})
}

// newChatManagerWith 装配聊天管理器：时间线/权限/退出经 emit 转发给前端；
// autoAllow 非 nil 且返回真时，权限请求自动放行（不弹窗）。
func newChatManagerWith(emit func(name string, data ...any), b chat.Backend, autoAllow func() bool) *chat.Manager {
	m := chat.NewManager(b,
		func(id string, u chat.Update) {
			emit("chat:update", map[string]any{"id": id, "update": u})
		},
		func(id string, r chat.PermissionRequest) {
			emit("chat:permission", map[string]any{"id": id, "request": r})
		},
		func(id string, code int, errMsg string) {
			emit("chat:exit", map[string]any{"id": id, "exitCode": code, "error": errMsg})
		})
	m.SetAutoAllowPermission(autoAllow)
	return m
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
	return m.Prompt(id, text)
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
