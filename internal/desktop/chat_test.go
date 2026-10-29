package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

type fakeChatConn struct {
	waitCh     chan struct{}
	once       sync.Once
	start      acp.SessionStart
	modelErr   error
	modelCalls []string
	configSet  []acp.SessionConfigOption // SetConfigOption 返回的配置全量状态
	promptGate chan struct{}             // 非 nil 时 Prompt 阻塞等待（desktop 测试模拟忙轮次）
	stopReply  string                    // 非 nil 时 Prompt 返回该 stopReason（模拟提前停止等）
}

func (c *fakeChatConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	return acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (c *fakeChatConn) NewSession(ctx context.Context, cwd string) (acp.SessionStart, error) {
	c.start.SessionID = "s1"
	return c.start, nil
}
func (c *fakeChatConn) LoadSession(ctx context.Context, sessionID, cwd string) (acp.SessionStart, error) {
	start := c.start
	if start.SessionID == "" {
		start.SessionID = sessionID
	}
	return start, nil
}
func (c *fakeChatConn) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	if c.promptGate != nil {
		<-c.promptGate
	}
	if c.stopReply != "" {
		return c.stopReply, nil
	}
	return "end_turn", nil
}
func (c *fakeChatConn) SetModel(ctx context.Context, sessionID, modelID string) error {
	c.modelCalls = append(c.modelCalls, modelID)
	return c.modelErr
}
func (c *fakeChatConn) SetConfigOption(ctx context.Context, sessionID, configID, value string) ([]acp.SessionConfigOption, error) {
	return c.configSet, nil
}
func (c *fakeChatConn) Authenticate(ctx context.Context, methodID string) error { return nil }
func (c *fakeChatConn) Cancel(sessionID string) error                           { return nil }
func (c *fakeChatConn) Stderr() string                                          { return "" }
func (c *fakeChatConn) Wait() (int, error)                                      { <-c.waitCh; return 0, nil }
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

// TestParseFollowPrompt 验证 #f 指令解析边界：整串 "#f"、"#f <消息>" 命中；
// "#fix something"、大写 "#F"、普通文本不触发。
func TestParseFollowPrompt(t *testing.T) {
	if rest, ok := parseFollowPrompt("#f"); !ok || rest != "" {
		t.Fatalf("#f 应命中且消息体为空: %q %v", rest, ok)
	}
	if rest, ok := parseFollowPrompt("  #f 继续修  "); !ok || rest != "继续修" {
		t.Fatalf("#f <消息> 应命中并 Trim: %q %v", rest, ok)
	}
	for _, in := range []string{"#fix something", "#F 继续修", "#继续修", "看看 #f 怎么用"} {
		if _, ok := parseFollowPrompt(in); ok {
			t.Fatalf("%q 不应被识别为 #f 指令", in)
		}
	}
}

func TestChatManagerEmitsUpdate(t *testing.T) {
	var mu sync.Mutex
	events := map[string]int{}
	emit := func(name string, data ...any) {
		mu.Lock()
		events[name]++
		mu.Unlock()
	}
	m := newChatManagerWith(emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
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

	// Home 用临时目录：mcpConfigDir 会真实写 MCP 配置文件，不能落在磁盘 D:\home
	home := t.TempDir()

	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\w`, Title: "t"},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}

	events := newEventLog()
	term := &stubTermBackend{}
	app := NewAppWith(Options{
		Home:      home,
		CachePath: filepath.Join(home, ".kshell", "cache", "index.json"),
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
		// 出站翻译与生产装配（newChatManager）同口径：manager 侧 ID 翻回原始 ID。
		app.opts.Chats = newChatManagerWith(events.emit, backend, func() bool {
			return app.snapshot().Config.PermissionMode == config.PermissionModeBypass
		}, nil, nil, app.originalChatID, nil)
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

// TestOpenSessionACPRefusesTerminalActive 终端页签在册时禁止抢拉 ACP。
func TestOpenSessionACPRefusesTerminalActive(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	tm := env.app.terminals()
	if _, err := tm.Open("session:s1", terminal.Info{
		Kind: terminal.KindSession, SessionID: "s1", Workspace: `D:\w`, Title: "t", ToolID: "claude",
	}, terminal.Spec{Path: "x"}, 80, 24); err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	if _, err := env.app.OpenSessionACP("s1"); err == nil || !strings.Contains(err.Error(), "err.hub.terminal_active") {
		t.Fatalf("终端在册应报 err.hub.terminal_active, got %v", err)
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

// TestOpenWorkspace_NotLoggedInFallsBackWithReason 验证未登录工具走偏好 ACP
// 新建会话时静默降级终端，且 Fallback 携带 wire key（前端据此提示
// 「未登录已降级，去登录」），而不是当作错误抛出。
func TestOpenWorkspace_NotLoggedInFallsBackWithReason(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.tools[0].Auth = string(providers.AuthOut)
	env.app.mu.Unlock()

	got, err := env.app.OpenWorkspace(`D:\w`, "claude")
	if err != nil {
		t.Fatalf("OpenWorkspace error: %v", err)
	}
	if got.Kind != "terminal" || got.Terminal == nil {
		t.Fatalf("未登录应降级终端: %+v", got)
	}
	if got.Fallback != launch.ErrNotLoggedIn.Error() {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, launch.ErrNotLoggedIn.Error())
	}
	if env.term.starts() != 1 {
		t.Fatalf("应启动一个终端进程, got %d", env.term.starts())
	}
}

// TestOpenWorkspaceACP_NotLoggedInRejected 验证显式 ACP 新建在未登录时原样
// 返回门禁错误且不起终端（用户明确要求 ACP，失败必须可见、不得静默降级）。
func TestOpenWorkspaceACP_NotLoggedInRejected(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.tools[0].Auth = string(providers.AuthOut)
	env.app.mu.Unlock()

	got, err := env.app.OpenWorkspaceACP(`D:\w`, "claude", "")
	if !errors.Is(err, launch.ErrNotLoggedIn) {
		t.Fatalf("未登录显式 ACP 应返回 ErrNotLoggedIn, got %v", err)
	}
	if got.Kind != "" || got.Terminal != nil || got.Chat != nil {
		t.Fatalf("显式 ACP 被拒不应产出任何会话: %+v", got)
	}
	if env.term.starts() != 0 {
		t.Fatalf("显式 ACP 被拒不应起终端进程, got %d", env.term.starts())
	}
}

// markACPOnly 把指定会话标记为仅能 ACP 激活（模拟 Cursor acp-sessions 来源）。
func markACPOnly(t *testing.T, app *App, id string) {
	t.Helper()
	app.mu.Lock()
	for i := range app.result.Sessions {
		if app.result.Sessions[i].ID == id {
			app.result.Sessions[i].ACPOnly = true
		}
	}
	app.mu.Unlock()
}

// TestOpenSessionACPOnlyBlocksFallback 验证 ACPOnly 会话（Cursor acp-sessions
// 来源）在偏好路径失败时一律显式报错、绝不回退终端——TUI resume 一个不在
// chats 库里的 ID 会静默起全新会话丢上下文，静默降级等于数据丢失。
func TestOpenSessionACPOnlyBlocksFallback(t *testing.T) {
	cases := []struct {
		name    string
		backend chat.Backend
		acp     *providers.ACPDetection
		wantErr error
	}{
		{"acp 不可用", fakeChatBackend{}, &providers.ACPDetection{Available: false}, launch.ErrACPUnavailable},
		{"起进程失败", fakeChatBackendErr{}, &providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"}, nil},
		// backend=nil：聊天管理器未就绪也要报错，而不是弹终端
		{"chats 未就绪", nil, &providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"}, errSessionACPOnly},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 不设 SessionModeACP：默认 tui 偏好下 ACPOnly 也要强制走 ACP 路径
			env := newChatTestApp(t, c.acp, c.backend)
			markACPOnly(t, env.app, "s1")

			got, err := env.app.OpenSession("s1")
			if err == nil {
				t.Fatalf("ACPOnly 会话激活失败应报错而非回退终端: %+v", got)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got.Kind != "" || got.Terminal != nil || got.Chat != nil {
				t.Fatalf("拦截分支不应产出任何会话: %+v", got)
			}
			if env.term.starts() != 0 {
				t.Fatalf("不得起终端进程, got %d", env.term.starts())
			}
		})
	}
}

// TestOpenSessionACPOnlyIgnoresTUIPreference 验证默认 tui 偏好下 ACPOnly 会话
// 仍强制走 ACP：按实际存储模式激活，而不是按全局偏好。
func TestOpenSessionACPOnlyIgnoresTUIPreference(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	markACPOnly(t, env.app, "s1")

	got, err := env.app.OpenSession("s1")
	if err != nil {
		t.Fatalf("OpenSession error: %v", err)
	}
	if got.Kind != "chat" || got.Chat == nil {
		t.Fatalf("ACPOnly 会话应强制走聊天路径: %+v", got)
	}
	if got.Chat.SessionID != "s1" {
		t.Fatalf("Chat.SessionID = %q, want s1", got.Chat.SessionID)
	}
	if env.term.starts() != 0 {
		t.Fatalf("不得起终端进程, got %d", env.term.starts())
	}
}

// TestResumeSessionACPOnlyRejected 验证 TUI 弹窗 resume 入口拒绝 ACPOnly 会话：
// chats 库里没有对应记录，resume 会静默起全新会话丢上下文。
func TestResumeSessionACPOnlyRejected(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{Available: false}, fakeChatBackend{})
	markACPOnly(t, env.app, "s1")

	if err := env.app.ResumeSession("s1"); !errors.Is(err, errSessionACPOnly) {
		t.Fatalf("err = %v, want errSessionACPOnly", err)
	}
	// 对照：普通会话不受拦截（此环境无窗口管理器，应走 errNotReady 而非 ACPOnly）
	if err := env.app.ResumeSession("s2"); err == nil || errors.Is(err, errSessionACPOnly) {
		t.Fatalf("普通会话不应被 ACPOnly 拦截, got %v", err)
	}
}

// TestOpenSessionTerminalACPOnlyRejected 验证内嵌终端入口同样拦截 ACPOnly
// 会话（防御纵深：该绑定一旦被前端接通即复现「静默丢上下文」）。
func TestOpenSessionTerminalACPOnlyRejected(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{Available: false}, fakeChatBackend{})
	markACPOnly(t, env.app, "s1")

	if _, err := env.app.OpenSessionTerminal("s1", 80, 24); !errors.Is(err, errSessionACPOnly) {
		t.Fatalf("err = %v, want errSessionACPOnly", err)
	}
	if env.term.starts() != 0 {
		t.Fatalf("不得起终端进程, got %d", env.term.starts())
	}
}

// TestShouldBlockTerminalFallback 表驱动锁定路由判断：只认 ACPOnly 标记。
func TestShouldBlockTerminalFallback(t *testing.T) {
	cases := []struct {
		name string
		s    providers.Session
		want bool
	}{
		{"普通会话放行", providers.Session{ID: "s1"}, false},
		{"ACPOnly 拦截", providers.Session{ID: "s2", ACPOnly: true}, true},
		{"其余字段不影响", providers.Session{ID: "s3", ToolID: "cursor", Path: "x"}, false},
	}
	for _, c := range cases {
		if got := shouldBlockTerminalFallback(c.s); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestACPOnlyErr 验证错误统一口：有原始错误透传保留原因，nil 退回稳定 key。
func TestACPOnlyErr(t *testing.T) {
	orig := errors.New("起进程失败")
	if got := acpOnlyErr(orig); got != orig {
		t.Fatalf("非 nil 应原样透传, got %v", got)
	}
	if got := acpOnlyErr(nil); !errors.Is(got, errSessionACPOnly) {
		t.Fatalf("nil 应退回 errSessionACPOnly, got %v", got)
	}
}
