package desktop

import (
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/terminal"
)

// hubRecorder 收集 emit 的事件名与 hub 是否收到 agent 通知（并发安全）。
type hubRecorder struct {
	mu    sync.Mutex
	names []string
	raws  []any // 与 names 对齐：单参数事件的首个负载（如 chat:meta 的 chat.Info）
}

func (r *hubRecorder) emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	var raw any
	if len(data) > 0 {
		raw = data[0]
	}
	r.raws = append(r.raws, raw)
}

// lastRaw 返回最近一次指定事件的单参数负载；未出现返回 nil。
func (r *hubRecorder) lastRaw(name string) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.names) - 1; i >= 0; i-- {
		if r.names[i] == name {
			return r.raws[i]
		}
	}
	return nil
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
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
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
	if u.SessionTitle != "t" {
		t.Fatalf("user 消息应带 SessionTitle 快照: %+v", u)
	}
	if u.SessionID != "s1" {
		t.Fatalf("user 消息应带 SessionID 快照: %+v", u)
	}
	if rec.count("hub:changed") == 0 {
		t.Fatalf("应 emit hub:changed，得到 %v", rec.names)
	}
	// 等 turn goroutine 收尾，避免 TempDir 清理与异步落盘竞争。
	waitHubKind(t, store, "agent")
}

// emitChatNotify 通过 allow 后落 agent 消息（含 assistant 回复正文与会话标题快照）。
func TestEmitChatNotifyRecordsHubMessage(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

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
	// 会话标题快照随消息落盘：会话关闭/重启后 chats 反查不到，气泡归因靠它
	if agent.SessionTitle != "t" {
		t.Fatalf("agent 消息应带 SessionTitle 快照: %+v", agent)
	}
	// 磁盘会话 ID 快照随消息落盘：会话关闭后「继续」按钮靠它精确 resume
	if agent.SessionID != "s1" {
		t.Fatalf("agent 消息应带 SessionID 快照: %+v", agent)
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

// waitHubEvent 等到第一条带指定 Event 的消息（ack 卡片断言用）。
func waitHubEvent(t *testing.T, store *hub.Store, event string) hub.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range store.List() {
			if m.Event == event {
				return m
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时未等到 hub event=%s 消息", event)
	return hub.Message{}
}

// waitHubText 等到第一条正文为 text 的用户消息。
func waitHubText(t *testing.T, store *hub.Store, text string) hub.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range store.List() {
			if m.Kind == "user" && m.Text == text {
				return m
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时未等到 hub 用户消息 %q", text)
	return hub.Message{}
}

// queueGateBackend 返回预置的连接（供测试预挂 promptGate）。
type queueGateBackend struct {
	conn *fakeChatConn
}

func (b *queueGateBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return b.conn, nil
}

// waitStatusDesktop 等待会话到达 running（desktop 测试用，chat 包的 waitStatus 不跨包）。
func waitStatusDesktop(t *testing.T, m *chat.Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.Get(id); ok && info.Status == "running" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session %s 未进入 running", id)
}

// SendChatPrompt 空闲投递：user 消息先落 queued，投递回调随后清空并补 ack_delivered。
func TestSendChatPromptImmediateAck(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)

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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, hasAck := findHubEvent(store, "ui.hub.ack_delivered")
		if !hasAck {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		ack, _ := findHubEvent(store, "ui.hub.ack_delivered")
		if ack.Kind != "agent" || ack.ChatID != info.ID || ack.Tool != "claude" {
			t.Fatalf("ack 卡片归因不符: %+v", ack)
		}
		// ack 出现说明投递回调已跑完：此时 user 消息状态必须已清空
		for _, mm := range store.List() {
			if mm.ID == u.ID && mm.Status != "" {
				t.Fatalf("投递后 status 应清空: %+v", mm)
			}
		}
		return
	}
	t.Fatal("空闲投递未补 ack_delivered")
}

// findHubEvent 查第一条带指定 Event 的消息（轮询辅助）。
func findHubEvent(store *hub.Store, event string) (hub.Message, bool) {
	for _, m := range store.List() {
		if m.Event == event {
			return m, true
		}
	}
	return hub.Message{}, false
}

// 忙时发送：user 消息 status=queued 且落 ack_queued；撤回成功后状态变 recalled，
// 且投递恢复后不补 ack_delivered（消息未投递给 agent）。
func TestSendChatPromptQueuedAndRecall(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	conn := &fakeChatConn{waitCh: make(chan struct{})}
	conn.promptGate = make(chan struct{})
	b := &queueGateBackend{conn: conn}
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatusDesktop(t, m, info.ID)
	if err := a.SendChatPrompt(info.ID, "second"); err != nil {
		t.Fatal(err)
	}
	u := waitHubText(t, store, "second")
	if u.Status != hub.StatusQueued {
		t.Fatalf("应标记 queued: %+v", u)
	}
	waitHubEvent(t, store, "ui.hub.ack_queued")

	// 撤回成功：状态变 recalled 且队列移除
	if err := a.RecallHubMessage(u.ID); err != nil {
		t.Fatalf("撤回: %v", err)
	}
	close(conn.promptGate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, mm := range store.List() {
			if mm.ID == u.ID && mm.Status == hub.StatusRecalled {
				found = true
			}
			if mm.Event == "ui.hub.ack_delivered" && mm.ChatID == info.ID {
				t.Fatalf("已撤回消息不得补 ack_delivered")
			}
		}
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("撤回后状态未更新")
}

// 空正文 agent 消息回退事件语义 wire key（hook 通道 Stop 载荷没有 message 字段）。
// hub 气泡用 ui.hub.event_* 第一人称 key；语义映射必须与
// frontend/src/lib/agentNotify.ts 的 eventLabel 保持一致。
func TestRecordHubAgentMessageEmptyTextFallsBackToEventKey(t *testing.T) {
	cases := []struct {
		event string
		want  string
	}{
		{"Stop", "ui.hub.event_done"},
		{"agent-turn-complete", "ui.hub.event_done"},
		{"session.idle", "ui.hub.event_done"},
		{"done", "ui.hub.event_done"},
		{"", "ui.hub.event_done"},
		{"Notification", "ui.hub.event_waiting_confirm"},
		{"attention", "ui.hub.event_waiting_confirm"},
		{"error", "ui.hub.event_error"},
		{"stopped", "ui.hub.event_stopped"},
		{"cancelled", "ui.hub.event_cancelled"},
	}
	for _, c := range cases {
		t.Run(c.event, func(t *testing.T) {
			a, store, _ := newHubTestApp(t)
			a.recordHubAgentMessage(agenthook.Payload{
				Tool: "codebuddy", Event: c.event, TermKey: "session:s1", Workspace: "d:/ws",
			})
			msgs := store.List()
			if len(msgs) != 1 {
				t.Fatalf("event %q: 应落 1 条，得到 %d", c.event, len(msgs))
			}
			m := msgs[0]
			if m.Text != c.want {
				t.Fatalf("event %q: 正文应回退为 %q，得到 %q", c.event, c.want, m.Text)
			}
			if m.Kind != "agent" || m.Event != c.event {
				t.Fatalf("event %q: 消息字段不符: %+v", c.event, m)
			}
		})
	}
}

// 非空正文原样保留不覆盖；全空白正文视为空，回退事件语义 wire key。
func TestRecordHubAgentMessageKeepsNonEmptyText(t *testing.T) {
	a, store, _ := newHubTestApp(t)
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "codebuddy", Event: "Stop", TermKey: "session:s1", Workspace: "d:/ws", Summary: "实际回复内容",
	})
	msgs := store.List()
	if len(msgs) != 1 || msgs[0].Text != "实际回复内容" {
		t.Fatalf("非空 Summary 应原样保留: %+v", msgs)
	}

	a2, store2, _ := newHubTestApp(t)
	a2.recordHubAgentMessage(agenthook.Payload{
		Tool: "codebuddy", Event: "Stop", TermKey: "session:s1", Workspace: "d:/ws", Summary: "   ",
	})
	msgs2 := store2.List()
	if len(msgs2) != 1 {
		t.Fatalf("应落 1 条，得到 %d", len(msgs2))
	}
	if msgs2[0].Text != "ui.hub.event_done" {
		t.Fatalf("全空白 Summary 应回退为 ui.hub.event_done，得到 %q", msgs2[0].Text)
	}
}

// CloseChat 后队列清空（Manager.Close），但 Hub store 里该会话的 queued 消息
// 没有任何状态修正路径——前端永远显示「排队中可撤回」而撤回必失败。
// 关闭会话必须把这些 queued 消息清为已失效（status 置空）。
func TestCloseChatClearsHubQueuedStatus(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	conn := &fakeChatConn{waitCh: make(chan struct{})}
	conn.promptGate = make(chan struct{})
	b := &queueGateBackend{conn: conn}
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatusDesktop(t, m, info.ID)
	if err := a.SendChatPrompt(info.ID, "second"); err != nil {
		t.Fatal(err)
	}
	u := waitHubText(t, store, "second")
	if u.Status != hub.StatusQueued {
		t.Fatalf("前置：应标记 queued: %+v", u)
	}

	if err := a.CloseChat(info.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 关闭后该会话的 queued 消息不得再显示排队中
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, mm := range store.List() {
			if mm.ID == u.ID {
				if mm.Status == hub.StatusQueued {
					t.Fatalf("关闭会话后 queued 消息应失效: %+v", mm)
				}
				found = true
			}
		}
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("消息丢失")
}

// 已送达的消息撤回报 err.hub.not_queued。
func TestRecallHubMessageNotQueued(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SendChatPrompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	u := waitHubKind(t, store, "user")
	err = a.RecallHubMessage(u.ID)
	if err == nil || err.Error() != "err.hub.not_queued" {
		t.Fatalf("已送达消息撤回应报 err.hub.not_queued, got %v", err)
	}
}

// 端到端：忙时发送排队 → turn 结束自动投递 → 状态清空 + 补 ack_delivered（不撤回路径）。
func TestSendChatPromptQueuedThenDelivered(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	conn := &fakeChatConn{waitCh: make(chan struct{})}
	conn.promptGate = make(chan struct{})
	b := &queueGateBackend{conn: conn}
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitStatusDesktop(t, m, info.ID)
	if err := a.SendChatPrompt(info.ID, "second"); err != nil {
		t.Fatal(err)
	}
	u := waitHubText(t, store, "second")
	if u.Status != hub.StatusQueued {
		t.Fatalf("应标记 queued: %+v", u)
	}
	waitHubEvent(t, store, "ui.hub.ack_queued")

	// 放行第一轮 → 队列里的 second 自动投递 → 状态清空 + ack_delivered
	close(conn.promptGate)
	deadline := time.Now().Add(2 * time.Second)
	lastCleared, lastAcked := false, false
	for time.Now().Before(deadline) {
		lastCleared, lastAcked = false, false
		for _, mm := range store.List() {
			if mm.ID == u.ID && mm.Status == "" {
				lastCleared = true
			}
			if mm.Event == "ui.hub.ack_delivered" && mm.ChatID == info.ID {
				lastAcked = true
			}
		}
		if lastCleared && lastAcked {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("投递后未完成状态清空+ack: cleared=%v, acked=%v", lastCleared, lastAcked)
}

// 终端 hook 通道（termKey = session:<磁盘会话ID>）的会话不在 chat manager 里，
// recordHubAgentMessage 应从终端管理器按 Key 反查标题补 SessionTitle 快照；
// 终端会话没有 chat ID，不得设 ChatID。
func TestRecordHubAgentMessageTerminalSessionTitle(t *testing.T) {
	store := hub.New(t.TempDir(), 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	a := NewAppWith(Options{
		HubStore:  store,
		Terminals: newTerminalManagerWith(func(string, ...any) {}, &stubTermBackend{}),
		Emit:      func(string, ...any) {},
	})
	m := a.terminals()
	// 打开一个恢复的历史会话终端：key 即 termKey（terminal.go 的 "session:"+sessionID 约定）
	if _, err := m.Open("session:s1", terminal.Info{
		Kind: terminal.KindSession, SessionID: "s1", Workspace: "D:/ws",
		Title: "修复登录页", ToolID: "codebuddy",
	}, terminal.Spec{Path: "x"}, 80, 24); err != nil {
		t.Fatalf("open: %v", err)
	}
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "codebuddy", Event: "Stop", TermKey: "session:s1", Workspace: "D:/ws",
	})
	msgs := store.List()
	if len(msgs) != 1 {
		t.Fatalf("应落 1 条，得到 %d", len(msgs))
	}
	if msgs[0].SessionTitle != "修复登录页" {
		t.Fatalf("SessionTitle = %q, 想要 修复登录页", msgs[0].SessionTitle)
	}
	if msgs[0].ChatID != "" {
		t.Fatalf("终端通道不应设 ChatID，得到 %q", msgs[0].ChatID)
	}
	if msgs[0].SessionID != "s1" {
		t.Fatalf("终端通道应从 termKey 派生 SessionID: %+v", msgs[0])
	}
}

// chat/终端管理器都查不到时，termKey 的 session: 前缀即磁盘会话 ID：
// 快照落盘供「继续」按钮在会话未激活时精确 resume；无前缀（如 new:N）不设。
func TestRecordHubAgentMessageDerivesSessionIDFromTermKey(t *testing.T) {
	a, store, _ := newHubTestApp(t)
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "done", TermKey: "session:s-42", Workspace: "D:/w", Summary: "ok",
	})
	msgs := store.List()
	if len(msgs) != 1 || msgs[0].SessionID != "s-42" {
		t.Fatalf("应从 termKey 派生 SessionID: %+v", msgs)
	}

	a2, store2, _ := newHubTestApp(t)
	a2.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "done", TermKey: "new:2", Workspace: "D:/w", Summary: "ok",
	})
	msgs2 := store2.List()
	if len(msgs2) != 1 || msgs2[0].SessionID != "" {
		t.Fatalf("无 session: 前缀不得设 SessionID: %+v", msgs2)
	}
}

// 重建后 agent 回复落盘 ChatID 必须是链头（旧 ID）：出站翻译把通知 ChatID 翻回
// 原始 ID，recordHubAgentMessage 反查 manager 拿到的是最新 ID，直接落盘会指向
// 前端不认识的新 ID，hub 归因断线。
func TestRecordHubAgentMessageLandsOnChainHead(t *testing.T) {
	b := &recordingBackend{}
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, a.originalChatID, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := info.ID
	// 构造别名链：old→new（模拟重建），出站通知带旧 ID
	a.rememberChatAlias(oldID, "c-fake-new")

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "done", TermKey: "session:s1", Workspace: "D:/w",
		ChatID: oldID, Summary: "回复",
	})
	msgs := store.List()
	if len(msgs) != 1 {
		t.Fatalf("应落 1 条, got %d", len(msgs))
	}
	if msgs[0].ChatID != oldID {
		t.Fatalf("agent 消息 ChatID 应落链头 %s, got %q", oldID, msgs[0].ChatID)
	}
}
