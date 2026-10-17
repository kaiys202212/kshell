package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
)

// fakeChatCaptureBackend 捕获 acp.Handler，供测试模拟 agent 侧发起权限请求；
// conn 非 nil 时返回注入的连接（便于预置 SessionStart 等）。
type fakeChatCaptureBackend struct {
	mu   sync.Mutex
	last acp.Handler
	conn *fakeChatConn
}

func (b *fakeChatCaptureBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	b.mu.Lock()
	b.last = h
	conn := b.conn
	b.mu.Unlock()
	if conn != nil {
		return conn, nil
	}
	return &fakeChatConn{waitCh: make(chan struct{})}, nil
}

func (b *fakeChatCaptureBackend) lastHandler() acp.Handler {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

// notifyRecorder 收集 emit 的事件与 notify:agent payload（并发安全）。
type notifyRecorder struct {
	mu       sync.Mutex
	names    []string
	payloads []agenthook.Payload
}

func (r *notifyRecorder) emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	if len(data) > 0 {
		if p, ok := data[0].(agenthook.Payload); ok {
			r.payloads = append(r.payloads, p)
		}
	}
}

func (r *notifyRecorder) count(name string) int {
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

// waitForNotify 等待出现 event 语义的 notify:agent payload（turn 经 goroutine 收尾）。
func waitForNotify(t *testing.T, r *notifyRecorder, event string) agenthook.Payload {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, p := range r.payloads {
			if eventLabelForChat(p.Event) == event {
				r.mu.Unlock()
				return p
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时未等到 event=%s 的 notify:agent", event)
	return agenthook.Payload{}
}

// eventLabelForChat 把聊天侧事件归一成 done/attention 语义，仅供测试过滤。
func eventLabelForChat(e string) string {
	if e == "Notification" {
		return "attention"
	}
	return e
}

// chat turn 正常结束：额外 emit notify:agent（done 语义），payload 带 chat 的 key/tool/workspace。
func TestChatTurnDoneEmitsAgentNotify(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "修登录页", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Info.Key 必须暴露 manager key：前端气泡点击据此定位中心区页签
	if info.Key != "session:s1" {
		t.Fatalf("Info.Key = %q, 想要 session:s1", info.Key)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}

	p := waitForNotify(t, rec, "done")
	if p.Event != "done" || p.Tool != "claude" || p.TermKey != "session:s1" || p.Workspace != "D:/w" {
		t.Fatalf("payload 不符: %+v", p)
	}
}

// done 通知应带上最后一轮 assistant 回复正文，用户才能直接看到 agent 说了什么。
func TestEmitChatNotifyCarriesAssistantReply(t *testing.T) {
	rec := &notifyRecorder{}
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 经捕获的 handler 推一条 assistant 分片进时间线（模拟 agent 回复）
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"回复内容占位"}}`)
	b.lastHandler().SessionUpdate("s1", raw)

	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	p := waitForNotify(t, rec, "done")
	if !strings.Contains(p.Summary, "回复内容占位") {
		t.Fatalf("done notify 未带 assistant 正文: %q", p.Summary)
	}
}

// stopped 通知正文为空时回退停止原因 wire key：agent 因 refusal/max_tokens 等
// 提前停止且无任何 assistant 分片时，此前 summary 为空，AgentHub 落一条空气泡
//（2026-10-15 用户反馈：ChatView 显示 Turn stopped，Hub 只有空消息）。
func TestChatStoppedEmptySummaryFallsBackToReason(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "claude"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	emitChatNotify(rec.emit, m, info.ID, "stopped", "", nil, nil, nil)
	p := waitForNotify(t, rec, "stopped")
	if p.Event != "stopped" || !strings.Contains(p.Summary, "ui.notify.turn_stopped_reason") {
		t.Fatalf("stopped 空 summary 应回退原因 wire key: %+v", p)
	}
}

// chat turn 出错：emit error 语义通知，summary 带错误文本。
func TestChatPromptErrorEmitsAgentNotify(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackendPromptErr{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "codex"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}

	p := waitForNotify(t, rec, "error")
	if p.Event != "error" || p.TermKey != "new:1" || !strings.Contains(p.Summary, "轮次失败") {
		t.Fatalf("payload 不符: %+v", p)
	}
}

// chat 权限请求：emit Notification 语义通知（等待确认），summary 用工具调用标题。
func TestChatPermissionEmitsAgentNotify(t *testing.T) {
	rec := &notifyRecorder{}
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "claude"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.lastHandler().RequestPermission(context.Background(), "sess-new", "r1", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "tc1", Title: "运行测试", Kind: "execute"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()

	p := waitForNotify(t, rec, "attention")
	if p.Event != "Notification" || p.TermKey != "new:1" || p.Tool != "claude" || p.Summary != "运行测试" {
		t.Fatalf("payload 不符: %+v", p)
	}
	if err := m.RespondPermission(info.ID, "r1", "allow"); err != nil {
		t.Fatal(err)
	}
	<-done
}

// chat 已关闭（id 查不到）时不再发通知：静默跳过。
func TestChatNotifySkipsUnknownChat(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	emitChatNotify(rec.emit, m, "c-missing", "done", "", nil, nil, nil)
	if n := rec.count("notify:agent"); n != 0 {
		t.Fatalf("未知 chat 不应 emit，得到 %d 次", n)
	}
}

// 超长中文回复按 rune 边界截断：按字节切会落在多字节字符中间，产出非法 UTF-8，
// JSON 序列化后变 U+FFFD 乱码（本项目回复正文以中文为主，必然命中）。
func TestLastAssistantTextTruncatesOnRuneBoundary(t *testing.T) {
	const maxLen = 2000
	// 每个汉字 3 字节，总长远超 maxLen，且 maxLen 处必落在字符中间
	text := strings.Repeat("中文回复", 800) // 3200 个 rune
	h := []chat.Update{{Type: "assistant", MessageID: "m1", Text: text}}

	got := lastAssistantText(h)
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8（出现乱码）: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != maxLen {
		t.Fatalf("截断长度 = %d 个 rune, 想要 %d", n, maxLen)
	}
}

// 空历史 / 无 assistant 分片时返回空串且不 panic。
func TestLastAssistantTextEmptyCases(t *testing.T) {
	if got := lastAssistantText(nil); got != "" {
		t.Fatalf("nil 历史应返回空串, got %q", got)
	}
	h := []chat.Update{
		{Type: "user", MessageID: "u1", Text: "你好"},
		{Type: "turn_done"},
	}
	if got := lastAssistantText(h); got != "" {
		t.Fatalf("无 assistant 分片应返回空串, got %q", got)
	}
}

// 未超长时不截断，且只取最后一轮（同 MessageID）分片拼接。
func TestLastAssistantTextJoinsLastTurnChunks(t *testing.T) {
	h := []chat.Update{
		{Type: "assistant", MessageID: "old", Text: "旧回复"},
		{Type: "user", MessageID: "u1", Text: "继续"},
		{Type: "assistant", MessageID: "new", Text: "第一段"},
		{Type: "assistant", MessageID: "new", Text: "第二段"},
	}
	if got := lastAssistantText(h); got != "第一段第二段" {
		t.Fatalf("got %q, 想要 第一段第二段", got)
	}
}

// chat 通知经同一套去重器：同一 payload 窗口内重复到达只 emit 一次
//（修复前 chat 路径直发绕过去重，属结构性旁路）。dedupe 为 nil 时不去重。
func TestChatNotifyDedupes(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "claude"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWaitWindow)
	emitChatNotify(rec.emit, m, info.ID, "Notification", "运行测试", d.allow, nil, nil)
	emitChatNotify(rec.emit, m, info.ID, "Notification", "运行测试", d.allow, nil, nil)
	if n := rec.count("notify:agent"); n != 1 {
		t.Fatalf("窗口内重复 chat 通知应只 emit 一次，得到 %d 次", n)
	}
}

// bypass 自动放行不得吞掉 AskUserQuestion：提问被静默代答等于剥夺用户决策权，
// 必须照常弹窗。工具名从 manager 已收到的 tool_call 更新（s.tools）反查——
// codebuddy 实测先发 session/update 再发 request_permission，顺序有保证。
func TestAwaitPermissionAutoAllowSkipsAskUserQuestion(t *testing.T) {
	rec := &notifyRecorder{}
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, func() bool { return true }, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "codebuddy"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	h := b.lastHandler()
	// agent 侧先发 tool_call 更新，再发权限请求（实测时序）
	h.SessionUpdate("sess-new", json.RawMessage(
		`{"sessionUpdate":"tool_call","toolCallId":"t1","name":"AskUserQuestion","title":"是否继续？"}`))

	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := h.RequestPermission(context.Background(), "sess-new", "r1", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "t1"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()

	// 短窗口内不得自动放行
	select {
	case out := <-done:
		t.Fatalf("AskUserQuestion 不应被 bypass 自动放行，得到 %+v", out)
	case <-time.After(150 * time.Millisecond):
	}
	if n := rec.count("chat:permission"); n != 1 {
		t.Fatalf("应发出 chat:permission 弹窗请求，得到 %d 次", n)
	}

	// 用户应答后正常返回
	if err := m.RespondPermission(info.ID, "r1", "allow"); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.OptionID != "allow" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("应答后未返回")
	}
}

// 回归保护：bypass 下普通工具（Bash）仍自动放行，不弹窗、不等待。
func TestAwaitPermissionAutoAllowsOrdinaryToolUnderBypass(t *testing.T) {
	rec := &notifyRecorder{}
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, func() bool { return true }, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	if _, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "codebuddy"},
		chat.Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	h := b.lastHandler()
	h.SessionUpdate("sess-new", json.RawMessage(
		`{"sessionUpdate":"tool_call","toolCallId":"t2","name":"Bash","title":"运行命令"}`))

	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := h.RequestPermission(context.Background(), "sess-new", "r2", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "t2"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()
	select {
	case out := <-done:
		if out.Outcome != "selected" || out.OptionID != "allow" {
			t.Fatalf("普通工具应自动放行，得到 %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("普通工具在 bypass 下应立即自动放行")
	}
	if n := rec.count("chat:permission"); n != 0 {
		t.Fatalf("自动放行不应弹窗，得到 %d 次", n)
	}
}

// ExitPlanMode 同属交互类：计划要用户批准，bypass 不得静默代答（同 AskUserQuestion）。
func TestAwaitPermissionAutoAllowSkipsExitPlanMode(t *testing.T) {
	rec := &notifyRecorder{}
	b := &fakeChatCaptureBackend{}
	m := newChatManagerWith(rec.emit, b, func() bool { return true }, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)

	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "D:/w", ToolID: "codebuddy"},
		chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	h := b.lastHandler()
	h.SessionUpdate("sess-new", json.RawMessage(
		`{"sessionUpdate":"tool_call","toolCallId":"t3","name":"ExitPlanMode","title":"计划待批准"}`))

	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := h.RequestPermission(context.Background(), "sess-new", "r3", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "t3"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()

	select {
	case out := <-done:
		t.Fatalf("ExitPlanMode 不应被 bypass 自动放行，得到 %+v", out)
	case <-time.After(150 * time.Millisecond):
	}
	if n := rec.count("chat:permission"); n != 1 {
		t.Fatalf("应发出 chat:permission 弹窗请求，得到 %d 次", n)
	}

	if err := m.RespondPermission(info.ID, "r3", "allow"); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.OptionID != "allow" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("应答后未返回")
	}
}

// fakeChatConnPromptErr 模拟一轮 Prompt 失败。
type fakeChatConnPromptErr struct {
	fakeChatConn
}

func (c *fakeChatConnPromptErr) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	return "", errors.New("轮次失败")
}

type fakeChatBackendPromptErr struct{}

func (fakeChatBackendPromptErr) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return &fakeChatConnPromptErr{fakeChatConn{waitCh: make(chan struct{})}}, nil
}

// turnEvent 把 stopReason 归纳为通知事件：只有 end_turn/空 才算完成；
// cancelled 是用户主动取消（中性）；其余（max_tokens/max_turn_requests/refusal/
// 未知值）是 agent 提前停止——此前一律报「任务完成」，正是「任务没做完却停了」
// 无任何提示的根因。
func TestTurnEventMapping(t *testing.T) {
	cases := map[string]string{
		"":                   "done",
		"end_turn":           "done",
		"cancelled":          "cancelled",
		"max_tokens":         "stopped",
		"max_turn_requests":  "stopped",
		"refusal":            "stopped",
		"some_future_reason": "stopped",
	}
	for stop, want := range cases {
		if got := turnEvent(stop); got != want {
			t.Fatalf("turnEvent(%q) = %q, want %q", stop, got, want)
		}
	}
}
