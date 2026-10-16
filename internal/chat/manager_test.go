package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

// fakeConn 按脚本记录调用；Wait 阻塞到 Close（模拟真实进程生命周期）。
type fakeConn struct {
	mu        sync.Mutex
	newStart  acp.SessionStart
	loadErr   error
	promptErr error
	stop      string
	stderr    string
	cancelled []string
	closed    bool
	waitCh    chan struct{}
	waitOnce  sync.Once

	promptGate   chan struct{}        // 非 nil 时 Prompt 阻塞等待，直到收到信号或 Close
	prompts      []string             // 记录每次 Prompt 的展开文本（断言投递顺序）
	promptBlocks [][]acp.ContentBlock // 记录每次 Prompt 的原始 blocks（断言附件形态）
	modelErr     error
	modelCalls   []string

	initGate   chan struct{} // 非 nil 时 Initialize 阻塞等待，模拟握手耗时
	initSet    bool
	initResult acp.InitializeResult
}

func (c *fakeConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	if c.initGate != nil {
		// 模拟真实 acp.Agent.call：ctx 取消时返回 DeadlineExceeded（握手挂死超时依赖此行为）
		select {
		case <-c.initGate:
		case <-ctx.Done():
			return acp.InitializeResult{}, ctx.Err()
		}
	}
	if c.initSet {
		return c.initResult, nil
	}
	return acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (c *fakeConn) NewSession(ctx context.Context, cwd string) (acp.SessionStart, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.newStart.SessionID == "" {
		c.newStart.SessionID = "sess-new"
	}
	return c.newStart, nil
}
func (c *fakeConn) LoadSession(ctx context.Context, sessionID, cwd string) (acp.SessionStart, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	start := c.newStart
	if start.SessionID == "" {
		start.SessionID = sessionID
	}
	return start, c.loadErr
}
func (c *fakeConn) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	c.mu.Lock()
	c.promptBlocks = append(c.promptBlocks, blocks)
	c.prompts = append(c.prompts, flattenBlocks(blocks))
	gate := c.promptGate
	c.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if c.stop == "" {
		c.stop = "end_turn"
	}
	return c.stop, c.promptErr
}

// promptCount 取某连接的 Prompt 调用次数（断言投递顺序用）。
func (c *fakeConn) promptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.prompts)
}
func (c *fakeConn) SetModel(ctx context.Context, sessionID, modelID string) error {
	c.mu.Lock()
	c.modelCalls = append(c.modelCalls, modelID)
	err := c.modelErr
	c.mu.Unlock()
	return err
}
func (c *fakeConn) Cancel(sessionID string) error {
	c.mu.Lock()
	c.cancelled = append(c.cancelled, sessionID)
	c.mu.Unlock()
	return nil
}
func (c *fakeConn) Stderr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderr
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
	// Info.SessionID 只存磁盘会话 ID：新建聊天的 ACP 内部 ID（sess-new）不写入，留空等扫描回填
	if info.Status != StatusReady || info.SessionID != "" {
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

// 异常 stopReason（refusal 等）时 turn_done 必须附带 agent stderr 尾部：
// 上游 CLI 把 429 等 API 错误映射成 refusal，具体原因只在 stderr 里。
func TestManagerTurnDoneAttachesStderrTail(t *testing.T) {
	b := &fakeBackend{conn: &fakeConn{
		waitCh: make(chan struct{}),
		stop:   "refusal",
		stderr: "2026-10-16 10:00:00 error: 429 Requests are too frequent. Please retry",
	}}
	c := &collector{}
	m := NewManager(b, c.onUpdate, c.onPermission, c.onExit)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		var done Update
		found := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				done, found = u, true
			}
		}
		c.mu.Unlock()
		if found {
			if done.StopReason != "refusal" {
				t.Fatalf("StopReason = %q", done.StopReason)
			}
			if !strings.Contains(done.Text, "429") {
				t.Fatalf("turn_done.Text 缺 stderr 内容：%q", done.Text)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no turn_done; updates=%+v", c.updates)
}

// 正常结束（end_turn）不附 stderr：避免把调试日志噪声灌进时间线。
func TestManagerTurnDoneEndTurnNoStderr(t *testing.T) {
	m, _, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		var done Update
		found := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				done, found = u, true
			}
		}
		c.mu.Unlock()
		if found {
			if done.Text != "" {
				t.Fatalf("end_turn 不应附 stderr，Text=%q", done.Text)
			}
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

func TestManagerAutoAllowPermission(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	m.SetAutoAllowPermission(func() bool { return true })
	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "auto", acp.RequestPermissionParams{
			SessionID: "sess-new",
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()
	select {
	case <-c.permCh:
		t.Fatal("auto-allow 不应发 chat:permission")
	case out := <-done:
		if out.Outcome != "selected" || out.OptionID != "allow" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auto-allow 未完成")
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

// TestManagerCloseRemovesFromList：主动关闭应从 List 摘除。
// 前端关闭页签后会在 scan:done 时用 ListChats 整表重建镜像；若 Close 只标 exited
// 仍留在 List 里，已关的聊天页签会被「复活」。
func TestManagerCloseRemovesFromList(t *testing.T) {
	m, _, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("重复 Close 应幂等: %v", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("Close 后 List 应为空, got %+v", got)
	}
}

// 握手挂死保护：Initialize 永不应答（已知 codebuddy --acp session/new 死锁场景）时，
// Open 必须在超时后返回错误，而不是永久挂起 Wails 绑定 goroutine。
func TestManagerOpenHandshakeTimeout(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.initGate = make(chan struct{}) // 卡死 Initialize，永不放行
	defer close(b.conn.initGate)          // 测试结束后放行，避免 goroutine 泄漏

	// 缩短握手超时，避免测试实等 30s
	old := handshakeTimeout
	handshakeTimeout = 500 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	done := make(chan error, 1)
	go func() {
		_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("握手挂死时 Open 应返回错误")
		}
		if !strings.Contains(err.Error(), "err.chat.handshake_timeout") {
			t.Fatalf("应返回握手超时 wire key, got %v", err)
		}
		// 失败会话不得留幽灵
		if got := m.List(); len(got) != 0 {
			t.Fatalf("failed open left ghost sessions: %+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Open 在握手挂死时未超时返回（永久挂起）")
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
	if _, err := m.Open("new:1", Info{}, Spec{Path: "x"}, ""); err == nil || err.Error() != "err.chat.unsupported_protocol|2" {
		t.Fatalf("want protocol version wire key, got %v", err)
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
	if _, err := m.Open("session:s1", Info{Kind: KindSession, Workspace: "/w"}, Spec{Path: "x"}, "s1"); err == nil || err.Error() != "err.chat.no_session_load" {
		t.Fatalf("want load-capability wire key, got %v", err)
	}
}

// chat 面向用户的错误统一走 wire key，前端按 key 翻译。
func TestChatErrorsWireKeys(t *testing.T) {
	if _, err := NewManager(nil, nil, nil, nil).Open("k", Info{}, Spec{Path: "x"}, ""); err == nil || err.Error() != "err.chat.not_ready" {
		t.Fatalf("not_ready = %v", err)
	}
	m, _, _ := newTestManager(t)
	if _, err := m.Open("", Info{}, Spec{Path: "x"}, ""); err == nil || err.Error() != "err.chat.empty_key" {
		t.Fatalf("empty_key = %v", err)
	}
	if err := m.Prompt("nope", "hi"); err == nil || err.Error() != "err.chat.gone" {
		t.Fatalf("gone = %v", err)
	}
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	m.mu.Lock()
	m.byID[info.ID].info.Status = StatusRunning
	m.mu.Unlock()
	// 忙时不再拒绝：Prompt 应入队并返回 nil（见 TestManagerPromptQueuesWhenRunning）。
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("busy prompt 应入队而非报错: %v", err)
	}
}

// promptSessionID 记录最近一次 Prompt 收到的协议 sessionID（不受 Info 绑定影响）。
func TestAttachSessionBindsNewChatInfo(t *testing.T) {
	m, b, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, Title: `D:\ws · CodeBuddy`}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if !m.AttachSession("disk-1", `d:/WS/`, "codebuddy", "修复页签标题", 1, time.Time{}) {
		t.Fatal("匹配的新建聊天应绑定成功")
	}
	got := m.List()[0]
	if got.ID != info.ID || got.SessionID != "disk-1" || got.Title != "修复页签标题" {
		t.Fatalf("绑定后 Info 未回填: %+v", got)
	}

	// 绑定只改 Info：协议层仍用 agent 返回的 sessionID（sess-new），Prompt 正常
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt after attach: %v", err)
	}
	b.conn.mu.Lock()
	gotNew := b.conn.newStart.SessionID
	b.conn.mu.Unlock()
	if gotNew != "sess-new" {
		t.Fatalf("协议 sessionID 不应被绑定改写: %q", gotNew)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		ready := m.byID[info.ID] != nil
		m.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestUpdateSessionTitleSyncsBound(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, ToolID: "codebuddy", Title: "占位"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !m.AttachSession("disk-1", `D:\ws`, "codebuddy", "", 0, time.Time{}) {
		t.Fatal("前置绑定失败")
	}
	if !m.UpdateSessionTitle("disk-1", "真实标题", 1) {
		t.Fatal("已绑定会话应能更新标题")
	}
	if got := m.List()[0]; got.Title != "真实标题" {
		t.Fatalf("Title = %q", got.Title)
	}
	if m.UpdateSessionTitle("disk-1", "真实标题", 1) {
		t.Fatal("标题未变时不应报告有更新")
	}
}

// TestUpdateSessionTitleFiresOnInfo 扫描回填标题命中时经 onInfo 推送更新后的
// Info（desktop 层据此 Emit chat:meta 并回填需求 sessionTitle 快照）；
// 未命中（无绑定会话/标题未变）不触发。
func TestUpdateSessionTitleFiresOnInfo(t *testing.T) {
	m, _, _ := newTestManager(t)
	infos := make(chan Info, 4)
	m.SetOnInfo(func(info Info) { infos <- info })
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, ToolID: "codebuddy", Title: "占位"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !m.AttachSession("disk-1", `D:\ws`, "codebuddy", "", 0, time.Time{}) {
		t.Fatal("前置绑定失败")
	}

	// 未命中：无该磁盘会话绑定
	if m.UpdateSessionTitle("disk-404", "别家标题", 1) {
		t.Fatal("未绑定会话不应报告有更新")
	}
	select {
	case info := <-infos:
		t.Fatalf("未命中不应触发 onInfo: %+v", info)
	default:
	}

	// 命中：推送的 Info 带新标题
	if !m.UpdateSessionTitle("disk-1", "真实标题", 1) {
		t.Fatal("已绑定会话应能更新标题")
	}
	select {
	case info := <-infos:
		if info.Title != "真实标题" || info.SessionID != "disk-1" {
			t.Fatalf("onInfo Info 不符: %+v", info)
		}
	case <-time.After(time.Second):
		t.Fatal("命中后应触发 onInfo")
	}

	// 标题未变：再次调用返回 false 且不触发
	if m.UpdateSessionTitle("disk-1", "真实标题", 1) {
		t.Fatal("标题未变时不应报告有更新")
	}
	select {
	case info := <-infos:
		t.Fatalf("标题未变不应触发 onInfo: %+v", info)
	default:
	}
}

func TestAttachSessionSkipsNonCandidates(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, ToolID: "codebuddy"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, c := range []struct{ ws, tool string }{{`D:\other`, "codebuddy"}, {`D:\ws`, "claude"}} {
		if m.AttachSession("disk-1", c.ws, c.tool, "标题", 1, time.Time{}) {
			t.Fatalf("ws=%q tool=%q 不应绑定", c.ws, c.tool)
		}
	}
	// KindSession 不改写
	if _, err := m.Open("session:s1", Info{Kind: KindSession, SessionID: "s1", Workspace: `D:\ws`}, Spec{Path: "x"}, "s1"); err != nil {
		t.Fatalf("open session: %v", err)
	}
	if m.AttachSession("disk-1", `D:\ws`, "claude", "标题", 1, time.Time{}) {
		t.Fatal("KindSession 聊天不应被改写")
	}
	for _, it := range m.List() {
		if it.Kind == KindSession && (it.SessionID != "s1" || it.SessionID == "disk-1") {
			t.Fatalf("KindSession Info 被意外改写: %+v", it)
		}
	}
}

// 新建聊天的 Info.SessionID 不应被 agent 返回的 ACP 会话 ID 污染：
// 那是协议内部 ID，与磁盘会话 ID 不是同一体系；Info.SessionID 留空等扫描回填。
func TestManagerOpenNewKeepsInfoSessionIDEmpty(t *testing.T) {
	m, _, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w", Title: "新会话"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if info.SessionID != "" {
		t.Fatalf("KindNew 的 Info.SessionID 应留空等回填, got %q", info.SessionID)
	}
}

func TestAttachSessionSkipsKnownIDs(t *testing.T) {
	m, _, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "claude"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	m.RememberKnownIDs(info.ID, []string{"s1"})
	if m.AttachSession("s1", `D:\ws`, "claude", "左侧旧标题", 12, time.Time{}) {
		t.Fatal("打开时已存在的会话不得绑到新建聊天")
	}
	if got := m.List()[0]; got.Title != "占位" || got.SessionID != "" {
		t.Fatalf("Info 被误绑: %+v", got)
	}
}

func TestAttachSessionSkipsOldCreatedAt(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "claude"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if m.AttachSession("s1", `D:\ws`, "claude", "左侧旧标题", 12, old) {
		t.Fatal("CreatedAt 早于打开时刻的旧会话不得绑定")
	}
}

func TestAttachSessionKeepsPlaceholderUntilMessages(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "claude"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !m.AttachSession("disk-new", `D:\ws`, "claude", "", 0, time.Time{}) {
		t.Fatal("无消息时仍应绑定 SessionID")
	}
	if got := m.List()[0]; got.SessionID != "disk-new" || got.Title != "占位" {
		t.Fatalf("无消息不得改标题: %+v", got)
	}
	if m.UpdateSessionTitle("disk-new", "真实标题", 0) {
		t.Fatal("messages=0 不得改标题")
	}
	if !m.UpdateSessionTitle("disk-new", "真实标题", 1) {
		t.Fatal("有消息后应改标题")
	}
	if got := m.List()[0]; got.Title != "真实标题" {
		t.Fatalf("Title = %q", got.Title)
	}
}

// 忙时排队：running 中再 Prompt 应入队并补时间线，不返回 errRunning。
func TestManagerPromptQueuesWhenRunning(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{}) // 让第一轮卡住，保持 running
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	waitStatus(t, m, info.ID, StatusRunning)

	// 第二轮在忙时应被接受（入队），不得返回 errRunning
	if err := m.Prompt(info.ID, "second"); err != nil {
		t.Fatalf("queued prompt should be accepted, got %v", err)
	}
	// 排队消息也要立刻进时间线，界面才看得到
	if !historyHasText(m.History(info.ID), "second") {
		t.Fatalf("queued user message missing from history")
	}
}

// turn_done 后自动投递队列下一条。
func TestManagerPromptDrainsQueueOnTurnDone(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "first")
	waitStatus(t, m, info.ID, StatusRunning)
	_ = m.Prompt(info.ID, "second")

	// 放行第一轮 → 收尾应自动发起第二轮（文本 "second"）
	b.conn.mu.Lock()
	gate := b.conn.promptGate
	b.conn.mu.Unlock()
	close(gate)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.conn.promptCount() >= 2 {
			b.conn.mu.Lock()
			got := b.conn.prompts
			b.conn.mu.Unlock()
			if got[1] != "second" {
				t.Fatalf("second prompt = %q, want %q", got[1], "second")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("queue not drained; prompts=%d", b.conn.promptCount())
}

// starting 态（握手可能数秒）期间 Prompt 会入队；Open 就绪后必须 drain 队列投递，
// 否则该消息永远等不到 turn_done 触发，静默卡死。
func TestManagerPromptQueuesWhileStarting(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.initGate = make(chan struct{}) // 卡住 Initialize，让会话停在 StatusStarting

	// Open 内部同步等待 Initialize，必须后台跑，否则测试自身死锁。
	opened := make(chan Info, 1)
	go func() {
		info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
		if err != nil {
			opened <- Info{}
			return
		}
		opened <- info
	}()

	// 会话在 Initialize 前已进 order/byID，状态为 StatusStarting；等它出现后取 id。
	waitStatus(t, m, "c1", StatusStarting)
	if err := m.Prompt("c1", "握手期消息"); err != nil {
		t.Fatalf("starting 期间 prompt 应入队: %v", err)
	}

	// 放行握手 → Open 完成并把队列首条投递出去
	close(b.conn.initGate)
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("Open 未完成")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.conn.mu.Lock()
		delivered := false
		for _, p := range b.conn.prompts {
			if p == "握手期消息" {
				delivered = true
			}
		}
		b.conn.mu.Unlock()
		if delivered {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("starting 入队消息未在就绪后投递; prompts=%v", b.conn.prompts)
}

// 排队消息在入队时已补过时间线，投递时不得再补一条，否则同一条用户消息出现两次。
func TestManagerDrainedMessageNotDuplicated(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "first")
	waitStatus(t, m, info.ID, StatusRunning)
	_ = m.Prompt(info.ID, "second")

	b.conn.mu.Lock()
	gate := b.conn.promptGate
	b.conn.mu.Unlock()
	close(gate)
	waitStatus(t, m, info.ID, StatusReady)

	n := 0
	for _, u := range m.History(info.ID) {
		if u.Type == "user" && u.Text == "second" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("排队消息在时间线出现 %d 次, 想要 1", n)
	}
}

// Close 必须清空队列：不应再投递已排队的消息。
func TestManagerCloseClearsQueue(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "first")
	waitStatus(t, m, info.ID, StatusRunning)
	_ = m.Prompt(info.ID, "second")
	_ = m.Close(info.ID)

	b.conn.mu.Lock()
	gate := b.conn.promptGate
	b.conn.mu.Unlock()
	close(gate)

	time.Sleep(100 * time.Millisecond)
	if n := b.conn.promptCount(); n != 1 {
		t.Fatalf("closed session should not drain queue; prompts=%d want 1", n)
	}
}

// 测试辅助：等待某会话到达指定状态。
func waitStatus(t *testing.T, m *Manager, id, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.Get(id); ok && info.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session %s never reached %s", id, want)
}

func historyHasText(h []Update, text string) bool {
	for _, u := range h {
		if u.Type == "user" && u.Text == text {
			return true
		}
	}
	return false
}

// 忙时 PromptHub 入队并返回 queued=true；撤回后队列条目不再投递。
func TestPromptHubQueueAndRecall(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)

	queued, err := m.PromptHub(info.ID, "second", "h1")
	if err != nil || !queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
	if removed := m.RecallQueued(info.ID, "h1"); !removed {
		t.Fatal("RecallQueued 应移除队列条目")
	}
	if m.RecallQueued(info.ID, "h1") {
		t.Fatal("重复撤回应返回 false")
	}
	close(b.conn.promptGate)
	// 等 first 投递完成，确认 second 不再投递
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.conn.promptCount() >= 1 {
			time.Sleep(100 * time.Millisecond) // 给潜在的误投递留窗口
			b.conn.mu.Lock()
			got := append([]string(nil), b.conn.prompts...)
			b.conn.mu.Unlock()
			for _, p := range got {
				if p == "second" {
					t.Fatalf("已撤回消息不得投递: %v", got)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("first 未投递")
}

// 空闲时 PromptHub 返回 queued=false；投递时触发 onHubDelivered。
func TestPromptHubImmediateDeliversWithCallback(t *testing.T) {
	m, _, _ := newTestManager(t)
	delivered := make(chan string, 4)
	m.SetOnHubCallbacks(func(hubID string) {}, func(hubID string) {
		if hubID != "" {
			delivered <- hubID
		}
	})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := m.PromptHub(info.ID, "hi", "h9")
	if err != nil || queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
	select {
	case got := <-delivered:
		if got != "h9" {
			t.Fatalf("delivered hubID = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onHubDelivered 未触发")
	}
}

// 竞态兜底：deliverTurn 期间会话转忙时改入队并触发 onHubQueued。
func TestPromptHubRaceFallbackTriggersQueuedCallback(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)

	queuedC := make(chan string, 4)
	m.SetOnHubCallbacks(func(hubID string) {
		if hubID != "" {
			queuedC <- hubID
		}
	}, func(hubID string) {})
	// 模拟竞态：直接调内部 deliverTurn（会话忙，应兜底入队并触发回调）
	qm := queuedMsg{blocks: []acp.ContentBlock{{Type: "text", Text: "raced"}}, hubMsgID: "h2"}
	if err := m.deliverTurn(info.ID, m.byID[info.ID], qm, true); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-queuedC:
		if got != "h2" {
			t.Fatalf("queued hubID = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onHubQueued 未触发")
	}
	m.RecallQueued(info.ID, "h2")
	close(b.conn.promptGate)
}

// ---- 状态活动跟随：提前返回 prompt 的 agent（codebuddy 等）随后仍推流水，
// 后端 Info.Status 必须跟随实际活动，否则 ListChats/chat:meta 携带的 ready
// 会在前端镜像同步时冲掉已拉回的 running（表现为任务在跑却显示「空闲」）。

func newIdleTestManager(t *testing.T) (*Manager, *fakeBackend, *collector, chan Info) {
	t.Helper()
	old := activityIdleFallback
	activityIdleFallback = 60 * time.Millisecond
	t.Cleanup(func() { activityIdleFallback = old })
	m, b, c := newTestManager(t)
	infoCh := make(chan Info, 16)
	m.SetOnInfo(func(info Info) {
		select {
		case infoCh <- info:
		default:
		}
	})
	return m, b, c, infoCh
}

func waitIdleReady(t *testing.T, m *Manager, id string) Info {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.Get(id); ok && info.Status == StatusReady {
			return info
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session %s never fell back to ready", id)
	return Info{}
}

// turn_done 后活动类更新（thought/assistant/tool/plan）把后端状态拉回 running；
// user 更新不算活动。
func TestManagerActivityPullsStatusBackToRunning(t *testing.T) {
	m, b, _, _ := newIdleTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)

	for _, raw := range []string{
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"t"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"a"}}`,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"T","status":"completed"}`,
		`{"sessionUpdate":"plan","entries":[{"content":"s","priority":"high","status":"pending"}]}`,
	} {
		// 等上一轮回退到 ready，再注入本轮更新，状态应立即拉回 running
		waitIdleReady(t, m, info.ID)
		b.last.SessionUpdate("sess-new", json.RawMessage(raw))
		waitStatus(t, m, info.ID, StatusRunning)
	}
	// user 分片不代表 agent 活动，不拉回
	waitIdleReady(t, m, info.ID)
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"u"}}`))
	if info, ok := m.Get(info.ID); !ok || info.Status != StatusReady {
		t.Fatalf("user 更新不应拉回 running: %+v", info)
	}
}

// drainInfoCh 非阻塞清空 meta 通道（首条 prompt 的标题改写也会推 meta，与回退无关）。
func drainInfoCh(infoCh chan Info) {
	for {
		select {
		case <-infoCh:
		default:
			return
		}
	}
}

// 静默回退：running（活动拉回）持续无活动且无在途信号时回 ready，并经 onInfo 推送
// （desktop 层据此 Emit chat:meta 同步前端）。
func TestManagerIdleFallbackMarksReadyAndPushesMeta(t *testing.T) {
	m, b, _, infoCh := newIdleTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"t"}}`))
	waitStatus(t, m, info.ID, StatusRunning)

	got := waitIdleReady(t, m, info.ID)
	// onInfo 必须携带回退后的 ready 状态
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case meta := <-infoCh:
			if meta.ID == info.ID && meta.Status == StatusReady {
				return
			}
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("静默回退未推送 meta; got=%+v lastMeta=%v", got, infoCh)
}

// 在途工具：turn_done（prompt 提前返回）时仍有未终态工具 → 保持 running；
// 工具终态且静默超时后才回 ready。
func TestManagerTurnDoneKeepsRunningWhileToolInFlight(t *testing.T) {
	m, b, c, infoCh := newIdleTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)
	// 子任务工具在途
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"sub1","title":"Task","status":"in_progress"}`))
	// 放行 prompt（提前返回）
	close(b.conn.promptGate)

	// turn_done 已发但状态不得回 ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := false
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				done = true
			}
		}
		c.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if info, ok := m.Get(info.ID); !ok || info.Status != StatusRunning {
		t.Fatalf("在途工具期间 turn_done 不应回 ready: %+v", info)
	}
	// 静默期过去也不得回 ready（在途工具是明确运行信号）
	time.Sleep(200 * time.Millisecond)
	if info, ok := m.Get(info.ID); !ok || info.Status != StatusRunning {
		t.Fatalf("在途工具期间不得静默回退: %+v", info)
	}

	// 工具终态 → 静默超时后回 ready 并推送 meta
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"sub1","status":"completed"}`))
	waitIdleReady(t, m, info.ID)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case meta := <-infoCh:
			if meta.ID == info.ID && meta.Status == StatusReady {
				return
			}
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("工具终态回退未推送 meta")
}

// 投递解耦：活动拉回 running 期间再发消息必须立即投递（agent 已结束当前 turn，
// ACP 层合法），不得因展示状态 running 卡死队列。
func TestManagerPromptDeliversWhileActivityRunning(t *testing.T) {
	m, b, _, _ := newIdleTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"t"}}`))
	waitStatus(t, m, info.ID, StatusRunning)

	if err := m.Prompt(info.ID, "second"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.conn.promptCount() >= 2 {
			b.conn.mu.Lock()
			got := append([]string(nil), b.conn.prompts...)
			b.conn.mu.Unlock()
			if got[1] != "second" {
				t.Fatalf("second prompt = %q", got[1])
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("活动 running 期间消息未立即投递; prompts=%d", b.conn.promptCount())
}

// Close 后静默回退定时器不得再触发（不推 meta、不复活状态）。
func TestManagerCloseStopsIdleFallback(t *testing.T) {	m, b, _, infoCh := newIdleTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"t"}}`))
	waitStatus(t, m, info.ID, StatusRunning)
	drainInfoCh(infoCh) // 清掉标题改写等历史 meta，只关注 Close 之后的新推送
	if err := m.Close(info.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case meta := <-infoCh:
		if meta.ID == info.ID {
			t.Fatalf("Close 后不得再推送 meta: %+v", meta)
		}
	default:
	}
}

// session/load 重放在握手期推来的历史工具更新不得登记为在途：
// 重放的执行态在本进程并不真实在跑，若污染 inFlightTools，
// turn_done 会因「在途非空」永久保持 running。
func TestManagerLoadReplayDoesNotPolluteInFlightTools(t *testing.T) {
	old := activityIdleFallback
	activityIdleFallback = 60 * time.Millisecond
	t.Cleanup(func() { activityIdleFallback = old })
	m, b, _ := newTestManager(t)
	b.conn.initGate = make(chan struct{})
	opened := make(chan Info, 1)
	go func() {
		info, err := m.Open("session:s1", Info{Kind: KindSession, SessionID: "s1", Workspace: "/w"}, Spec{Path: "x"}, "s1")
		if err != nil {
			opened <- Info{}
			return
		}
		opened <- info
	}()
	waitStatus(t, m, "c1", StatusStarting)
	// 重放：历史里未终态的工具
	b.last.SessionUpdate("s1", json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"old1","title":"Task","status":"in_progress"}`))
	close(b.conn.initGate)
	info := <-opened
	if info.ID == "" {
		t.Fatal("open failed")
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)
	// 静默期后仍应 ready（无幽灵在途把状态钉在 running）
	time.Sleep(150 * time.Millisecond)
	if got, ok := m.Get(info.ID); !ok || got.Status != StatusReady {
		t.Fatalf("重放污染在途工具导致状态异常: %+v", got)
	}
}

// exited 是终态：进程退出后迟到的活动更新不得把状态复活成 running。
func TestManagerActivityDoesNotResurrectExited(t *testing.T) {
	m, b, _ := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b.conn.crash()
	waitStatus(t, m, info.ID, StatusExited)
	b.last.SessionUpdate("sess-new", json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"t"}}`))
	if got, ok := m.Get(info.ID); !ok || got.Status != StatusExited {
		t.Fatalf("exited 被活动更新复活: %+v", got)
	}
}
