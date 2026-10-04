package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

type fakeChatConn struct {
	waitCh chan struct{}
	once   sync.Once
}

func (c *fakeChatConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	return acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (c *fakeChatConn) NewSession(ctx context.Context, cwd string) (string, error) { return "s1", nil }
func (c *fakeChatConn) LoadSession(ctx context.Context, sessionID, cwd string) error {
	return nil
}
func (c *fakeChatConn) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	return "end_turn", nil
}
func (c *fakeChatConn) Cancel(sessionID string) error { return nil }
func (c *fakeChatConn) Wait() (int, error)            { <-c.waitCh; return 0, nil }
func (c *fakeChatConn) Close() error {
	c.once.Do(func() { close(c.waitCh) })
	return nil
}

type fakeChatBackend struct{}

func (fakeChatBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return &fakeChatConn{waitCh: make(chan struct{})}, nil
}

// fakeChatBackendErr 模拟 ACP 起进程/握手失败，用于验证回退终端的 Fallback 原因。
type fakeChatBackendErr struct{}

func (fakeChatBackendErr) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return nil, errors.New("起进程失败")
}

func TestChatManagerEmitsUpdate(t *testing.T) {
	var mu sync.Mutex
	events := map[string]int{}
	emit := func(name string, data ...any) {
		mu.Lock()
		events[name]++
		mu.Unlock()
	}
	m := newChatManagerWith(emit, fakeChatBackend{}, nil)
	t.Cleanup(m.CloseAll) // 关掉 watchExit goroutine，避免测试泄漏
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := events["chat:update"]
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no chat:update emitted")
}

// chatTestEnv 是「统一打开入口」测试的桩件：会话/工具直接落位（跳过真实扫描），
// 终端与聊天管理器都走真实装配函数 + 桩后端，事件走真实 emit 路径。
type chatTestEnv struct {
	app    *App
	term   *stubTermBackend
	events *eventLog
}

// newChatTestApp 装配 App：acp 决定工具是否可用 ACP；backend 为 nil 时不装聊天管理器
// （用于验证「无聊天管理器」也应回退终端）。Claude provider 提供终端回退所需的 ResumeCmd/NewSessionCmd。
func newChatTestApp(t *testing.T, acpDet *providers.ACPDetection, backend chat.Backend) *chatTestEnv {
	t.Helper()

	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\w`, Title: "t"},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}

	events := newEventLog()
	term := &stubTermBackend{}
	app := NewAppWith(Options{
		Home:      "D:/home",
		CachePath: "D:/home/.kshell/cache/index.json",
		Providers: []providers.Provider{providers.Claude{}},
		Terminals: newTerminalManagerWith(events.emit, term),
		Emit:      events.emit,
	})

	app.mu.Lock()
	app.result = res
	app.tools = []discovery.Tool{{
		ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true, ACP: acpDet,
	}}
	if backend != nil {
		app.opts.Chats = newChatManagerWith(events.emit, backend, func() bool {
			return app.snapshot().Config.PermissionMode == config.PermissionModeBypass
		})
	}
	app.mu.Unlock()

	// Shutdown 会 CloseAll 终端与聊天，watchExit/读循环随连接关闭收尾。
	t.Cleanup(func() { app.Shutdown(context.Background()) })
	return &chatTestEnv{app: app, term: term, events: events}
}

// TestOpenSessionACP 验证偏好 ACP 且可用时会话走聊天路径。
func TestOpenSessionACP(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.mu.Unlock()

	got, err := env.app.OpenSession("s1")
	if err != nil {
		t.Fatalf("OpenSession error: %v", err)
	}
	if got.Kind != "chat" || got.Chat == nil {
		t.Fatalf("ACP 可用应走聊天: %+v", got)
	}
	if got.Terminal != nil || got.Fallback != "" {
		t.Fatalf("聊天路径不应带终端/回退信息: %+v", got)
	}
	if got.Chat.SessionID != "s1" || got.Chat.Workspace != `D:\w` {
		t.Fatalf("Chat 未带上会话上下文: %+v", got.Chat)
	}
	if env.term.starts() != 0 {
		t.Fatalf("聊天路径不应起终端进程, got %d", env.term.starts())
	}
}

// TestOpenSessionDefaultTUI 验证默认 tui 偏好下即使 ACP 可用也走终端。
func TestOpenSessionDefaultTUI(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})

	got, err := env.app.OpenSession("s1")
	if err != nil {
		t.Fatalf("OpenSession error: %v", err)
	}
	if got.Kind != "terminal" || got.Terminal == nil {
		t.Fatalf("默认 tui 应走终端: %+v", got)
	}
}

// TestOpenSessionACPExplicit 验证显式 ACP 入口忽略 tui 偏好。
func TestOpenSessionACPExplicit(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})

	got, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP error: %v", err)
	}
	if got.Kind != "chat" || got.Chat == nil {
		t.Fatalf("显式 ACP 应走聊天: %+v", got)
	}
}

// TestOpenSessionFallbackTerminal 验证 ACP 不可用时回退内嵌终端。
// 聊天管理器仍在（非 nil），回退是由 ACP 不可用触发，而非缺管理器。
func TestOpenSessionFallbackTerminal(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{Available: false}, fakeChatBackend{})

	got, err := env.app.OpenSession("s1")
	if err != nil {
		t.Fatalf("OpenSession error: %v", err)
	}
	if got.Kind != "terminal" || got.Terminal == nil {
		t.Fatalf("ACP 不可用应回退终端: %+v", got)
	}
	if got.Chat != nil {
		t.Fatalf("终端路径不应返回聊天: %+v", got.Chat)
	}
	if env.term.starts() != 1 {
		t.Fatalf("应启动一个终端进程, got %d", env.term.starts())
	}
}

// TestOpenWorkspace_ACPUnavailableFallsBack 验证工作区新建在 ACP 不可用时回退终端，
// 且该路径按设计不带 Fallback 原因（探测不到 ACP ≠ 起进程失败）。
func TestOpenWorkspace_ACPUnavailableFallsBack(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{Available: false}, fakeChatBackend{})

	got, err := env.app.OpenWorkspace(`D:\w`, "claude")
	if err != nil {
		t.Fatalf("OpenWorkspace error: %v", err)
	}
	if got.Kind != "terminal" || got.Terminal == nil {
		t.Fatalf("ACP 不可用应回退终端: %+v", got)
	}
	if got.Fallback != "" {
		t.Fatalf("ACP 不可用路径不应带 Fallback 原因, got %q", got.Fallback)
	}
	if env.term.starts() != 1 {
		t.Fatalf("应启动一个终端进程, got %d", env.term.starts())
	}
}

// TestOpenSessionACPFailureFallsBack 验证 ACP 声明可用但起进程失败时回退终端，
// 并带上失败原因供前端提示。
func TestOpenSessionACPFailureFallsBack(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackendErr{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.mu.Unlock()

	got, err := env.app.OpenSession("s1")
	if err != nil {
		t.Fatalf("OpenSession error: %v", err)
	}
	if got.Kind != "terminal" || got.Terminal == nil {
		t.Fatalf("ACP 起进程失败应回退终端: %+v", got)
	}
	if got.Fallback == "" {
		t.Fatal("起进程失败的回退应带 Fallback 原因")
	}
}
