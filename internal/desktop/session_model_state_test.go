package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// modelCallSink 聚合 backend 产出的连接上的 SetModel 调用（rebuild 会换连接）。
type modelCallSink struct {
	mu    sync.Mutex
	calls []string
}

func (s *modelCallSink) add(calls []string) {
	s.mu.Lock()
	s.calls = append(s.calls, calls...)
	s.mu.Unlock()
}

func (s *modelCallSink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

// sinkChatConn 包 fakeChatConn，把 SetModel 调用同步进 sink。
type sinkChatConn struct {
	*fakeChatConn
	sink *modelCallSink
}

func (c *sinkChatConn) SetModel(ctx context.Context, sessionID, modelID string) error {
	c.sink.add([]string{modelID})
	return c.fakeChatConn.SetModel(ctx, sessionID, modelID)
}

type opencodeStateBackend struct {
	sink  *modelCallSink
	model acp.SessionModelState
}

func (b *opencodeStateBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	c := &fakeChatConn{
		start:  acp.SessionStart{SessionID: "s1", Models: &b.model},
		waitCh: make(chan struct{}),
	}
	return &sinkChatConn{fakeChatConn: c, sink: b.sink}, nil
}

// opencodeChatTestEnv 与 chatTestEnv 同构，但会话/工具换成 opencode 并装配
// Layout.State（会话级模型记忆的落盘目录）。
type opencodeChatTestEnv struct {
	app        *App
	events     *eventLog
	stateRoot  string
	statePath  string
	modelCalls *modelCallSink
}

func newOpenCodeChatTestEnv(t *testing.T, models []acp.ModelInfo, current string) *opencodeChatTestEnv {
	t.Helper()
	home := t.TempDir()
	stateRoot := filepath.Join(home, ".kshell", "state")

	sessions := []providers.Session{
		{ID: "s1", ToolID: "opencode", Workspace: `D:\w`, Title: "t"},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}

	events := newEventLog()
	term := &stubTermBackend{}
	sink := &modelCallSink{}
	backend := &opencodeStateBackend{
		sink: sink,
		model: acp.SessionModelState{
			AvailableModels: models,
			CurrentModelID:  current,
		},
	}
	app := NewAppWith(Options{
		Home:      home,
		CachePath: filepath.Join(home, ".kshell", "cache", "index.json"),
		Providers: []providers.Provider{providers.Opencode{}},
		Terminals: newTerminalManagerWith(events.emit, term),
		Emit:      events.emit,
	})
	app.mu.Lock()
	app.result = res
	app.tools = []discovery.Tool{{
		ID: "opencode", Name: "OpenCode", BinPath: "opencode", Installed: true,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "opencode-agent-acp"},
	}}
	app.opts.Layout.State = stateRoot
	app.opts.Chats = newChatManagerWith(events.emit, backend, func() bool {
		return app.snapshot().Config.PermissionMode == config.PermissionModeBypass
	}, nil, nil, app.originalChatID, nil)
	app.mu.Unlock()

	t.Cleanup(func() { app.Shutdown(context.Background()) })
	return &opencodeChatTestEnv{
		app: app, events: events, stateRoot: stateRoot,
		statePath:  filepath.Join(stateRoot, "opencode-session-models.json"),
		modelCalls: sink,
	}
}

func (env *opencodeChatTestEnv) readModelState(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(env.statePath)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("state 文件损坏: %v", err)
	}
	return m
}

func opencodeModels() []acp.ModelInfo {
	return []acp.ModelInfo{
		{ModelID: "opencode/big-pickle", Name: "Big Pickle"},
		{ModelID: "kshell-p1/glm-5.3-flash", Name: "GLM"},
	}
}

// 开 opencode 历史会话：state 里记过该会话上次模型且在可用列表、与 Current
// 不同 → 自动 SetModel 原生 ID 恢复。
func TestOpenSessionACPRestoresOpenCodeModelFromState(t *testing.T) {
	env := newOpenCodeChatTestEnv(t, opencodeModels(), "opencode/big-pickle")
	if err := os.MkdirAll(env.stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.statePath, []byte(`{"s1":"kshell-p1/glm-5.3-flash"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(env.modelCalls.all()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if calls := env.modelCalls.all(); len(calls) != 1 || calls[0] != "kshell-p1/glm-5.3-flash" {
		t.Fatalf("应 SetModel(kshell-p1/glm-5.3-flash): %v", calls)
	}
}

// state 里没记录该会话：不 SetModel、不挡激活。
func TestOpenSessionACPOpenCodeSkipsRestoreWithoutState(t *testing.T) {
	env := newOpenCodeChatTestEnv(t, opencodeModels(), "opencode/big-pickle")

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if calls := env.modelCalls.all(); len(calls) != 0 {
		t.Fatalf("无 state 记录不应 SetModel: %v", calls)
	}
}

// SetChatModel 成功后：opencode 会话要把「磁盘会话 ID→本次切换的原生模型」
// 落盘 state，供下次恢复。
func TestSetChatModelOpenCodeRecordsSessionModel(t *testing.T) {
	env := newOpenCodeChatTestEnv(t, opencodeModels(), "opencode/big-pickle")
	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	chatID := opened.Chat.ID

	if err := env.app.SetChatModel(chatID, "kshell-p1/glm-5.3-flash"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	got := env.readModelState(t)
	if got["s1"] != "kshell-p1/glm-5.3-flash" {
		t.Fatalf("SetChatModel 后 state = %v, 要有 s1→kshell-p1/glm-5.3-flash", got)
	}
}

// 非 opencode 工具（claude）SetChatModel 不得写该 state 文件。
func TestSetChatModelClaudeDoesNotRecordSessionModel(t *testing.T) {
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, fakeChatBackend{})
	env.app.mu.Lock()
	env.app.opts.Layout.State = filepath.Join(env.app.opts.Home, ".kshell", "state")
	env.app.mu.Unlock()

	opened, err := env.app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	if err := env.app.SetChatModel(opened.Chat.ID, "m2"); err != nil {
		t.Fatalf("SetChatModel: %v", err)
	}
	statePath := filepath.Join(env.app.opts.Home, ".kshell", "state", "opencode-session-models.json")
	if _, err := os.Stat(statePath); err == nil {
		t.Fatal("claude 会话不得写 opencode-session-models.json")
	}
}

// Layout.State 未装配时 record/lookup 空转不 panic。
func TestRecordOpenCodeSessionModelGuards(t *testing.T) {
	app := NewAppWith(Options{Home: t.TempDir()})
	if got := app.lookupOpenCodeSessionModel("s1"); got != "" {
		t.Fatalf("未装配 Layout.State 时 lookup 应为空, got %q", got)
	}
	app.recordOpenCodeSessionModel("s1", "m")
	if got := app.lookupOpenCodeSessionModel("s1"); got != "" {
		t.Fatalf("record 未装配时应跳过, got %q", got)
	}
}

// record 覆盖旧值：同会话切两次模型，恢复用最后一次。
func TestRecordOpenCodeSessionModelOverwrites(t *testing.T) {
	home := t.TempDir()
	app := NewAppWith(Options{Home: home})
	app.mu.Lock()
	app.opts.Layout.State = filepath.Join(home, ".kshell", "state")
	app.mu.Unlock()

	app.recordOpenCodeSessionModel("sd1", "m1")
	app.recordOpenCodeSessionModel("sd1", "m2")
	if got := app.lookupOpenCodeSessionModel("sd1"); got != "m2" {
		t.Fatalf("后写应覆盖, got %q", got)
	}
}
