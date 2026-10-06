package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
)

// fakeChatCaptureBackend 捕获 acp.Handler，供测试模拟 agent 侧发起权限请求。
type fakeChatCaptureBackend struct {
	mu   sync.Mutex
	last acp.Handler
}

func (b *fakeChatCaptureBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	b.mu.Lock()
	b.last = h
	b.mu.Unlock()
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
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil)
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

// chat turn 出错：emit error 语义通知，summary 带错误文本。
func TestChatPromptErrorEmitsAgentNotify(t *testing.T) {
	rec := &notifyRecorder{}
	m := newChatManagerWith(rec.emit, fakeChatBackendPromptErr{}, nil)
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
	m := newChatManagerWith(rec.emit, b, nil)
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
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil)
	t.Cleanup(m.CloseAll)

	emitChatNotify(rec.emit, m, "c-missing", "done", "")
	if n := rec.count("notify:agent"); n != 0 {
		t.Fatalf("未知 chat 不应 emit，得到 %d 次", n)
	}
}

// fakeChatConnPromptErr 模拟一轮 Prompt 失败。
type fakeChatConnPromptErr struct {
	fakeChatConn
}

func (c *fakeChatConnPromptErr) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	return "", errors.New("轮次失败")
}

type fakeChatBackendPromptErr struct{}

func (fakeChatBackendPromptErr) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return &fakeChatConnPromptErr{fakeChatConn{waitCh: make(chan struct{})}}, nil
}
