package weixin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/hub"
)

// fakeExec 是命令层的内存假执行端。
type fakeExec struct {
	sessions []hub.SessionRef
	current  hub.SessionRef
	hasCur   bool
	sendErr  error
}

func (f *fakeExec) ListSessions() []hub.SessionRef             { return f.sessions }
func (f *fakeExec) CurrentSession() (hub.SessionRef, bool)     { return f.current, f.hasCur }
func (f *fakeExec) SwitchTo(string) error                      { return nil }
func (f *fakeExec) OpenSession(string) (hub.SessionRef, error) { return hub.SessionRef{}, nil }
func (f *fakeExec) EasyNew() (hub.SessionRef, error)           { return hub.SessionRef{}, nil }
func (f *fakeExec) NewMenuOptions() (tools, workspaces, models []hub.MenuOption) {
	return nil, nil, nil
}
func (f *fakeExec) CreateNew(string, string, string) (hub.SessionRef, error) {
	return hub.SessionRef{}, nil
}
func (f *fakeExec) Stop(string) error                                   { return nil }
func (f *fakeExec) ListModels(string) ([]hub.MenuOption, string, error) { return nil, "", nil }
func (f *fakeExec) SetModel(string, string) error                       { return nil }
func (f *fakeExec) SearchSessions(string) []hub.SessionRef              { return nil }
func (f *fakeExec) Send(string, string) error                           { return f.sendErr }
func (f *fakeExec) GrantPermission(string, string, bool) error          { return nil }
func (f *fakeExec) Status() hub.ConsoleStatus                           { return hub.ConsoleStatus{} }

// reqRecord 记录一次 HTTP 请求的路径与解码后的 JSON 体。
type reqRecord struct {
	path string
	body map[string]any
}

// recorder 是测试服务端的请求记录器；sendResp 非空时定制 sendmessage 响应
// （call 从 1 计，status<=0 按 200 处理，body 空串按 {} 处理）。
type recorder struct {
	mu       sync.Mutex
	reqs     []reqRecord
	sendResp func(call int) (status int, body string)
}

func (rc *recorder) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc.mu.Lock()
		defer rc.mu.Unlock()
		rec := reqRecord{path: r.URL.Path}
		if r.Body != nil {
			var m map[string]any
			if err := json.NewDecoder(r.Body).Decode(&m); err == nil {
				rec.body = m
			}
		}
		rc.reqs = append(rc.reqs, rec)
		status, body := 200, "{}"
		if r.URL.Path == "/ilink/bot/sendmessage" && rc.sendResp != nil {
			// 当前请求已 append 进 rc.reqs，filterSendLocked 的长度即本次序号。
			status, body = rc.sendResp(len(rc.filterSendLocked()))
			if status <= 0 {
				status = 200
			}
			if body == "" {
				body = "{}"
			}
		}
		if status != 200 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}
}

func (rc *recorder) all() []reqRecord {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]reqRecord(nil), rc.reqs...)
}

func (rc *recorder) filterSendLocked() []reqRecord {
	var out []reqRecord
	for _, r := range rc.reqs {
		if r.path == "/ilink/bot/sendmessage" {
			out = append(out, r)
		}
	}
	return out
}

// sends 返回全部 sendmessage 请求。
func (rc *recorder) sends() []reqRecord {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]reqRecord(nil), rc.filterSendLocked()...)
}

// newBridge 构造指向测试服务端的桥（zh 文案、no-op sleep）。
func newBridge(t *testing.T, wxid string, ex hub.Executor, h http.HandlerFunc) *Bridge {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	b := NewBridge(NewClient("tok", srv.URL), ex, BridgeConfig{Wxid: wxid, Language: "zh-CN"})
	b.sleep = func(time.Duration) {} // 重试等待注入 no-op，避免真实等待
	return b
}

// sendMsgFields 从 sendmessage 请求体提取文本与 context_token。
func sendMsgFields(t *testing.T, body map[string]any) (text, token string) {
	t.Helper()
	msg, ok := body["msg"].(map[string]any)
	if !ok {
		t.Fatalf("请求体缺少 msg 包裹: %v", body)
	}
	token, _ = msg["context_token"].(string)
	items, _ := msg["item_list"].([]any)
	if len(items) != 1 {
		t.Fatalf("item_list 应只有一项: %v", msg["item_list"])
	}
	item := items[0].(map[string]any)
	ti := item["text_item"].(map[string]any)
	text, _ = ti["text"].(string)
	return text, token
}

func TestBridgeWhitelist(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u-me", ex, rc.handler(t))
	b.HandleInbound("u-other", "ct", "你好")
	b.HandleInbound("", "ct", "hi")
	if n := len(rc.all()); n != 0 {
		t.Errorf("非白名单入站后 client 请求次数 = %d, 期望 0", n)
	}
}

func TestBridgeInboundList(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, sessions: []hub.SessionRef{
		{ChatID: "c1", Workspace: "/a/proj", Tool: "claude", Title: "t1"},
	}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "#l")
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("sendmessage 次数 = %d, 期望 1", len(sends))
	}
	text, token := sendMsgFields(t, sends[0].body)
	if !strings.Contains(text, "1. [proj]") {
		t.Errorf("列表文本缺少编号特征: %q", text)
	}
	if token != "ct-1" {
		t.Errorf("context_token = %q, 期望 ct-1", token)
	}
}

func TestBridgeInboundSendEcho(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	// Send 成功：无回显消息。
	b.HandleInbound("u1", "ct-1", "普通消息")
	if n := len(rc.sends()); n != 0 {
		t.Errorf("Send 成功时 sendmessage 次数 = %d, 期望 0", n)
	}
	// Send 失败：回显「发送失败」。
	ex.sendErr = errors.New("boom")
	b.HandleInbound("u1", "ct-2", "普通消息")
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("Send 失败时 sendmessage 次数 = %d, 期望 1", len(sends))
	}
	text, _ := sendMsgFields(t, sends[0].body)
	if !strings.Contains(text, "发送失败") {
		t.Errorf("失败回显缺少「发送失败」: %q", text)
	}
}

func TestBridgeDeliverHubMessage(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	// 先入站建立发送上下文。
	b.HandleInbound("u1", "ct-9", "你好")

	// kind=user 跳过。
	b.DeliverHubMessage(hub.Message{Kind: "user", Text: "用户消息"})
	// event 前缀 ui.hub. 跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Event: "ui.hub.ack_queued", Text: "whatever"})
	// wire key 形态文本跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "hub.welcome"})
	// 已撤回跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Status: hub.StatusRecalled, Text: "撤回消息"})
	if n := len(rc.sends()); n != 0 {
		t.Fatalf("应跳过的消息触发了 %d 次 sendmessage, 期望 0", n)
	}

	// agent 正常推送，带标题前缀，context_token 用最近入站的。
	b.DeliverHubMessage(hub.Message{Kind: "agent", SessionTitle: "标题", Text: "hello world"})
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("sendmessage 次数 = %d, 期望 1", len(sends))
	}
	text, token := sendMsgFields(t, sends[0].body)
	if text != "[标题] hello world" {
		t.Errorf("出站文本 = %q, 期望 [标题] hello world", text)
	}
	if token != "ct-9" {
		t.Errorf("context_token = %q, 期望 ct-9", token)
	}
}

func TestBridgePendingOnSessionExpired(t *testing.T) {
	rc := &recorder{sendResp: func(call int) (int, string) {
		if call == 1 { // 首次 sendmessage 返回 -14
			return 200, `{"code":-14,"message":"expired"}`
		}
		return 200, "{}"
	}}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好") // Send 成功，无 sendmessage，ctx 已记录

	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "待发内容"})
	if b.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, 期望 1", b.PendingCount())
	}
	if b.Undelivered() != 0 {
		t.Errorf("排队中不应计入失败: Undelivered = %d", b.Undelivered())
	}

	// 服务端恢复后再入站 → pending 自动补发。
	b.HandleInbound("u1", "ct-2", "再来一条")
	sends := rc.sends()
	if len(sends) != 2 { // 第 1 次是失败的那条，第 2 次是补发
		t.Fatalf("sendmessage 次数 = %d, 期望 2", len(sends))
	}
	text, token := sendMsgFields(t, sends[1].body)
	if text != "待发内容" {
		t.Errorf("补发文本 = %q, 期望 待发内容", text)
	}
	if token != "ct-1" { // 补发用各自存的 token，不是最新入站的
		t.Errorf("补发 context_token = %q, 期望 ct-1", token)
	}
	if b.PendingCount() != 0 {
		t.Errorf("补发后 PendingCount = %d, 期望 0", b.PendingCount())
	}
	if b.Undelivered() != 0 {
		t.Errorf("补发全部成功后 Undelivered = %d, 期望 0", b.Undelivered())
	}
}

func TestBridgeSendRetry(t *testing.T) {
	t.Run("前2次失败第3次成功", func(t *testing.T) {
		rc := &recorder{sendResp: func(call int) (int, string) {
			if call <= 2 {
				return 500, "{}"
			}
			return 200, "{}"
		}}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.HandleInbound("u1", "ct-1", "你好")
		b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "重试内容"})
		if n := len(rc.sends()); n != 3 {
			t.Errorf("sendmessage 次数 = %d, 期望 3（重试 2 次）", n)
		}
		if b.Undelivered() != 0 {
			t.Errorf("重试成功后 Undelivered = %d, 期望 0", b.Undelivered())
		}
	})

	t.Run("三次全失败计一次", func(t *testing.T) {
		rc := &recorder{sendResp: func(int) (int, string) { return 500, "{}" }}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.HandleInbound("u1", "ct-1", "你好")
		b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "失败内容"})
		if n := len(rc.sends()); n != 3 {
			t.Errorf("sendmessage 次数 = %d, 期望 3", n)
		}
		if b.Undelivered() != 1 {
			t.Errorf("Undelivered = %d, 期望 1", b.Undelivered())
		}
		if b.PendingCount() != 0 {
			t.Errorf("普通失败不应入 pending: PendingCount = %d", b.PendingCount())
		}
	})
}

func TestBridgeTruncate(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好")
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: strings.Repeat("字", 2000)})
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("sendmessage 次数 = %d, 期望 1", len(sends))
	}
	text, _ := sendMsgFields(t, sends[0].body)
	runes := []rune(text)
	if len(runes) > 1510 {
		t.Errorf("截断后 rune 数 = %d, 期望 ≤1510", len(runes))
	}
	if !strings.Contains(text, "完整内容见桌面端") {
		t.Errorf("截断文本缺少提示后缀: %q", text[len(text)-40:])
	}
}
