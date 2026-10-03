package chat

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

// fakeConn 按脚本记录调用；Wait 阻塞到 Close（模拟真实进程生命周期）。
type fakeConn struct {
	mu        sync.Mutex
	newID     string
	loadErr   error
	promptErr error
	stop      string
	cancelled []string
	closed    bool
	waitCh    chan struct{}
	waitOnce  sync.Once

	initSet    bool
	initResult acp.InitializeResult
}

func (c *fakeConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	if c.initSet {
		return c.initResult, nil
	}
	return acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (c *fakeConn) NewSession(ctx context.Context, cwd string) (string, error) {
	if c.newID == "" {
		c.newID = "sess-new"
	}
	return c.newID, nil
}
func (c *fakeConn) LoadSession(ctx context.Context, sessionID, cwd string) error { return c.loadErr }
func (c *fakeConn) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	if c.stop == "" {
		c.stop = "end_turn"
	}
	return c.stop, c.promptErr
}
func (c *fakeConn) Cancel(sessionID string) error {
	c.mu.Lock()
	c.cancelled = append(c.cancelled, sessionID)
	c.mu.Unlock()
	return nil
}
func (c *fakeConn) Wait() (int, error) {
	<-c.waitCh
	return 0, nil
}
func (c *fakeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.waitOnce.Do(func() { close(c.waitCh) })
	return nil
}

// crash 模拟进程异常退出：只结束 Wait，不经过 Manager.Close。
func (c *fakeConn) crash() {
	c.waitOnce.Do(func() { close(c.waitCh) })
}

type fakeBackend struct {
	conn *fakeConn
	err  error
	last acp.Handler
}

func (b *fakeBackend) Start(spec Spec, h acp.Handler) (Conn, error) {
	b.last = h
	if b.err != nil {
		return nil, b.err
	}
	return b.conn, nil
}

type collector struct {
	mu         sync.Mutex
	updates    []Update
	permission *PermissionRequest
	permCh     chan string
}

func (c *collector) onUpdate(id string, u Update) {
	c.mu.Lock()
	c.updates = append(c.updates, u)
	c.mu.Unlock()
}
func (c *collector) onPermission(id string, r PermissionRequest) {
	c.mu.Lock()
	c.permission = &r
	c.mu.Unlock()
	if c.permCh != nil {
		c.permCh <- r.RequestID
	}
}
func (c *collector) onExit(id string, code int, errMsg string) {}

func newTestManager(t *testing.T) (*Manager, *fakeBackend, *collector) {
	t.Helper()
	b := &fakeBackend{conn: &fakeConn{waitCh: make(chan struct{})}}
	c := &collector{}
	m := NewManager(b, c.onUpdate, c.onPermission, c.onExit)
	return m, b, c
}

func TestManagerOpenNewAndPrompt(t *testing.T) {
	m, _, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w", Title: "新会话"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if info.Status != StatusReady || info.SessionID != "sess-new" {
		t.Fatalf("info = %+v", info)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		found := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				found = true
			}
		}
		c.mu.Unlock()
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no turn_done; updates=%+v", c.updates)
}

func TestManagerPromptEmitsUserMessage(t *testing.T) {
	m, _, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hello world"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	// 实时轮次不回显用户消息，客户端必须自己补一条（否则响应期间界面全空）
	got := m.History(info.ID)
	found := false
	for _, u := range got {
		if u.Type == "user" && u.Text == "hello world" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user message not appended; history=%+v", got)
	}
}

func TestManagerLoadReplay(t *testing.T) {
	m, b, _ := newTestManager(t)
	info, err := m.Open("session:s1", Info{Kind: KindSession, SessionID: "s1", Workspace: "/w"}, Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	raw := json.RawMessage(`{"sessionUpdate":"user_message_chunk","messageId":"m0","content":{"type":"text","text":"old"}}`)
	b.last.SessionUpdate("s1", raw)
	got := m.History(info.ID)
	if len(got) == 0 || got[0].Type != "user" || got[0].Text != "old" {
		t.Fatalf("history = %+v", got)
	}
}

func TestManagerPermissionRoundTrip(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "77", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "c1", Title: "Write"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()
	reqID := <-c.permCh
	if err := m.RespondPermission(info.ID, reqID, "allow"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	select {
	case out := <-done:
		if out.Outcome != "selected" || out.OptionID != "allow" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission not resolved")
	}
}

func TestManagerCloseCancelsPendingPermission(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "88", acp.RequestPermissionParams{SessionID: "sess-new"})
		done <- out
	}()
	<-c.permCh
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case out := <-done:
		if out.Outcome != "cancelled" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending permission not cancelled on close")
	}
}

func TestManagerOpenBackendError(t *testing.T) {
	b := &fakeBackend{conn: &fakeConn{waitCh: make(chan struct{})}, err: errors.New("boom")}
	m := NewManager(b, nil, nil, nil)
	if _, err := m.Open("new:1", Info{}, Spec{Path: "x"}, ""); err == nil {
		t.Fatal("want error")
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("failed open left ghost sessions: %+v", got)
	}
}

func TestManagerProcessExitCancelsPendingPermission(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "99", acp.RequestPermissionParams{SessionID: "sess-new"})
		done <- out
	}()
	<-c.permCh
	b.conn.crash()
	select {
	case out := <-done:
		if out.Outcome != "cancelled" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending permission not cancelled on process exit")
	}
}

func TestNormalizeUpdateIgnoresUnknownVariant(t *testing.T) {
	if _, ok := normalizeUpdate(json.RawMessage(`{"sessionUpdate":"usage_update"}`)); ok {
		t.Fatal("want ok=false for unknown variant")
	}
}

func TestManagerSynthesizesMessageIDForChunks(t *testing.T) {
	m, b, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	chunk := func(text string) json.RawMessage {
		return json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + text + `"}}`)
	}
	b.last.SessionUpdate("sess-new", chunk("a"))
	b.last.SessionUpdate("sess-new", chunk("b"))
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"T"}`))
	b.last.SessionUpdate("sess-new", chunk("c"))

	h := m.History(info.ID)
	if len(h) != 4 {
		t.Fatalf("history len = %d: %+v", len(h), h)
	}
	if h[0].MessageID == "" || h[0].MessageID != h[1].MessageID {
		t.Fatalf("连续同类分片应共用合成 id: %q vs %q", h[0].MessageID, h[1].MessageID)
	}
	if h[3].MessageID == "" || h[3].MessageID == h[0].MessageID {
		t.Fatalf("tool_call 后的分片应换新 id: %q (首个 %q)", h[3].MessageID, h[0].MessageID)
	}
}

func TestManagerRejectsUnsupportedProtocol(t *testing.T) {
	b := &fakeBackend{conn: &fakeConn{waitCh: make(chan struct{}), initSet: true, initResult: acp.InitializeResult{ProtocolVersion: 2}}}
	m := NewManager(b, nil, nil, nil)
	if _, err := m.Open("new:1", Info{}, Spec{Path: "x"}, ""); err == nil {
		t.Fatal("want protocol version error")
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("failed open left ghost sessions: %+v", got)
	}
}

func TestManagerRejectsLoadWithoutCapability(t *testing.T) {
	b := &fakeBackend{conn: &fakeConn{
		waitCh:     make(chan struct{}),
		initSet:    true,
		initResult: acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: false}},
	}}
	m := NewManager(b, nil, nil, nil)
	if _, err := m.Open("session:s1", Info{Kind: KindSession, Workspace: "/w"}, Spec{Path: "x"}, "s1"); err == nil {
		t.Fatal("want load-capability error")
	}
}
