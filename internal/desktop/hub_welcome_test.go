package desktop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/providers"
)

// newWelcomeTestApp 构造带 HubStore 与 ACP 聊天桩的 App：供新建欢迎词的集成断言。
// 装配方式与 newChatTestApp 一致（会话直接落位跳过真实扫描），额外注入 HubStore。
func newWelcomeTestApp(t *testing.T) (*App, *hub.Store, *hubRecorder) {
	t.Helper()
	store := hub.New(t.TempDir(), 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	rec := &hubRecorder{}
	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\w`, Title: "t"},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}
	term := &stubTermBackend{}
	// Home 用临时目录：mcpConfigDir 会真实写 MCP 配置文件，不能落在磁盘 D:\home
	home := t.TempDir()
	app := NewAppWith(Options{
		Home:      home,
		CachePath: filepath.Join(home, ".kshell", "cache", "index.json"),
		Providers: []providers.Provider{providers.Claude{}},
		Terminals: newTerminalManagerWith(rec.emit, term),
		HubStore:  store,
		Emit:      rec.emit,
	})
	app.mu.Lock()
	app.result = res
	app.tools = []discovery.Tool{{
		ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true,
		ACP: &providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"},
	}}
	app.opts.Chats = newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, app.recordHubAgentMessage)
	app.mu.Unlock()
	t.Cleanup(func() { app.Shutdown(context.Background()) })
	return app, store, rec
}

// OpenWorkspaceACP 新建成功后写一条 agent 欢迎词进 hub：Kind=agent、正文是
// 稳定 wire key+工具名插值（Go 不写自然语言），归因字段与该会话一致；
// resume/load 已有会话不写。
func TestOpenWorkspaceACPRecordsHubWelcome(t *testing.T) {
	app, store, rec := newWelcomeTestApp(t)

	got, err := app.OpenWorkspaceACP(`D:\w`, "claude")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got.Kind != "chat" || got.Chat == nil {
		t.Fatalf("应走聊天路径: %+v", got)
	}
	m := waitHubKind(t, store, "agent")
	if m.ChatID != got.Chat.ID || m.TermKey != got.Chat.Key ||
		m.Tool != "claude" || m.Workspace != `D:\w` {
		t.Fatalf("欢迎词归因字段不符: %+v", m)
	}
	if m.Text != "hub.welcome|Claude Code" {
		t.Fatalf("欢迎词正文应为 wire key+工具名插值: %q", m.Text)
	}
	if rec.count("hub:changed") == 0 {
		t.Fatalf("应 emit hub:changed，得到 %v", rec.names)
	}

	// resume 已有会话：不写欢迎词（live 里仍只有新建那一条 agent 消息）
	if _, err := app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, msg := range store.List() {
		if msg.Kind == "agent" && strings.HasPrefix(msg.Text, "hub.welcome|") && msg.ChatID != got.Chat.ID {
			t.Fatalf("resume 不应写欢迎词: %+v", msg)
		}
	}
}
