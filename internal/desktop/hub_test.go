package desktop

import (
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
)

// hubRecorder 收集 emit 的事件名与 hub 是否收到 agent 通知（并发安全）。
type hubRecorder struct {
	mu    sync.Mutex
	names []string
}

func (r *hubRecorder) emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
}

func (r *hubRecorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, v := range r.names {
		if v == name {
			n++
		}
	}
	return n
}

// newHubTestApp 构造带 hub.Store（temp 目录）与事件收集的 App。
func newHubTestApp(t *testing.T) (*App, *hub.Store, *hubRecorder) {
	t.Helper()
	store := hub.New(t.TempDir(), 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	rec := &hubRecorder{}
	a := NewAppWith(Options{HubStore: store, Emit: rec.emit})
	return a, store, rec
}

// handleAgentNotify 通过过滤/去重后落一条 agent 消息并 emit hub:changed。
func TestHandleAgentNotifyRecordsHubMessage(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	a.handleAgentNotify(agenthook.Payload{
		Tool: "claude", Event: "Stop", TermKey: "session:s1", Workspace: "D:/w", Summary: "done",
	})
	msgs := store.List()
	if len(msgs) != 1 {
		t.Fatalf("应落 1 条，得到 %d", len(msgs))
	}
	m := msgs[0]
	if m.Kind != "agent" || m.Tool != "claude" || m.TermKey != "session:s1" ||
		m.Workspace != "D:/w" || m.Text != "done" || m.Event != "Stop" {
		t.Fatalf("agent 消息字段不符: %+v", m)
	}
	if rec.count("hub:changed") != 1 {
		t.Fatalf("应 emit 一次 hub:changed，得到 %v", rec.names)
	}
}

// 被去重拦截的通知不落盘（Hub 只记用户真正会看到的事件）。
func TestHandleAgentNotifyDedupSkipsHub(t *testing.T) {
	a, store, _ := newHubTestApp(t)
	p := agenthook.Payload{Tool: "claude", Event: "Stop", TermKey: "session:s1", Workspace: "D:/w", Summary: "done"}
	a.handleAgentNotify(p)
	a.handleAgentNotify(p) // 窗口内重复
	if got := len(store.List()); got != 1 {
		t.Fatalf("去重后应只落 1 条，得到 %d", got)
	}
}

// SendChatPrompt 落一条 user 消息。
func TestSendChatPromptRecordsHubUserMessage(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := a.SendChatPrompt(info.ID, "帮我修复登录"); err != nil {
		t.Fatal(err)
	}
	u := waitHubKind(t, store, "user")
	if u.Kind != "user" || u.Text != "帮我修复登录" || u.ChatID != info.ID ||
		u.Tool != "claude" || u.Workspace != "D:/w" {
		t.Fatalf("user 消息字段不符: %+v", u)
	}
	if rec.count("hub:changed") == 0 {
		t.Fatalf("应 emit hub:changed，得到 %v", rec.names)
	}
	// 等 turn goroutine 收尾，避免 TempDir 清理与异步落盘竞争。
	waitHubKind(t, store, "agent")
}

// emitChatNotify 通过 allow 后落 agent 消息（含 assistant 回复正文）。
func TestEmitChatNotifyRecordsHubMessage(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	agent := waitHubKind(t, store, "agent")
	if agent.TermKey != "session:s1" || agent.Tool != "claude" || agent.Workspace != "D:/w" {
		t.Fatalf("chat agent 消息字段不符: %+v", agent)
	}
}

func waitHubKind(t *testing.T, store *hub.Store, kind string) hub.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range store.List() {
			if m.Kind == kind {
				return m
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时未等到 hub %s 消息", kind)
	return hub.Message{}
}
