package weixin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/hub"
)

// fakeExec 是命令层的内存假执行端。
type fakeExec struct {
	sessions  []hub.SessionRef
	current   hub.SessionRef
	hasCur    bool
	sendErr   error
	sentTexts []string // Send 收到的文本（按序记录）
	// switchCalls / currentCalls：SwitchTo 与 CurrentSession 调用记录
	//（引用回复切换语义断言用）。
	switchCalls  []string
	currentCalls int
}

func (f *fakeExec) ListSessions() []hub.SessionRef { return f.sessions }
func (f *fakeExec) CurrentSession() (hub.SessionRef, bool) {
	f.currentCalls++
	return f.current, f.hasCur
}
func (f *fakeExec) SwitchTo(chatID string) error {
	f.switchCalls = append(f.switchCalls, chatID)
	return nil
}
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
func (f *fakeExec) Send(chatID, text string) error {
	f.sentTexts = append(f.sentTexts, text)
	return f.sendErr
}
func (f *fakeExec) GrantPermission(string, string, bool) error { return nil }
func (f *fakeExec) Status() hub.ConsoleStatus                  { return hub.ConsoleStatus{} }
func (f *fakeExec) RecentMessages(int) []hub.Message           { return nil }

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

// TestBridgeLearnWxid 验证学习机制：wxid 未配置时首条入站消息的发送者被
// 记为白名单用户（回调通知 + 放行处理），后续其他用户仍被拒。
func TestBridgeLearnWxid(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}, sendErr: errors.New("boom")}
	b := newBridge(t, "", ex, rc.handler(t)) // wxid 空 = 待学习
	var learned string
	b.SetOnLearnWxid(func(wxid string) { learned = wxid })

	// 首条消息：学习 u1 并放行（Send 失败回显一条 sendmessage）。
	b.HandleInbound("u1", "ct-1", "hi")
	if learned != "u1" {
		t.Fatalf("学习回调 wxid = %q, 期望 u1", learned)
	}
	if b.Wxid() != "u1" {
		t.Fatalf("学习后 Wxid() = %q, 期望 u1", b.Wxid())
	}
	if n := len(rc.sends()); n != 1 {
		t.Fatalf("首条消息应放行产生 1 次 sendmessage, got %d", n)
	}

	// 第二条不同 wxid 被拒：不再新增 sendmessage。
	b.HandleInbound("u2", "ct-2", "hello")
	if n := len(rc.sends()); n != 1 {
		t.Fatalf("学习后其他用户应被拒, sendmessage 次数 = %d, 期望仍为 1", n)
	}

	// 空发送者不参与学习也不放行。
	b2 := newBridge(t, "", ex, rc.handler(t))
	b2.HandleInbound("", "ct", "hi")
	if b2.Wxid() != "" {
		t.Fatalf("空发送者不应触发学习: Wxid() = %q", b2.Wxid())
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
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	// Send 成功：回一条已接收确认（agent 稍后回复，微信侧先给回执），
	// 带【会话名】归因前缀（console 无回复 = 文本已转发，2026-10-18 用户反馈）。
	b.HandleInbound("u1", "ct-1", "普通消息")
	sends := rc.sends()
	if n := len(sends); n != 1 {
		t.Fatalf("Send 成功时 sendmessage 次数 = %d, 期望 1（已接收确认）", n)
	}
	text, _ := sendMsgFields(t, sends[0].body)
	if !strings.Contains(text, "【会话A】收到") {
		t.Errorf("成功回显缺少【会话名】前缀与已接收确认: %q", text)
	}
	// Send 失败：回显「发送失败」而非确认。
	ex.sendErr = errors.New("boom")
	b.HandleInbound("u1", "ct-2", "普通消息")
	sends = rc.sends()
	if len(sends) != 2 {
		t.Fatalf("Send 失败时 sendmessage 次数 = %d, 期望 2", len(sends))
	}
	text, _ = sendMsgFields(t, sends[1].body)
	if !strings.Contains(text, "发送失败") {
		t.Errorf("失败回显缺少「发送失败」: %q", text)
	}
}

func TestBridgeDeliverHubMessage(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	// 先入站建立发送上下文（会产生一条已接收确认）。
	b.HandleInbound("u1", "ct-9", "你好")
	b.FlushForTest()
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("入站确认 sendmessage 次数 = %d, 期望 1", len(sends))
	}

	// kind=user 跳过。
	b.DeliverHubMessage(hub.Message{Kind: "user", Text: "用户消息"})
	// event 前缀 ui.hub. 跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Event: "ui.hub.ack_queued", Text: "whatever"})
	// wire key 形态文本跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "hub.welcome"})
	// hub.welcome|<工具名>（含空格逃过 isWireKey）：欢迎词由命令层覆盖，跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", SessionTitle: "标题", Text: "hub.welcome|Claude Code"})
	// hook 通道空正文回退：Text 是 ui.hub.* wire key 但 Event 非空，同样跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Event: "Stop", Text: "ui.hub.event_done"})
	// 已撤回跳过。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Status: hub.StatusRecalled, Text: "撤回消息"})
	if n := len(rc.sends()); n != 1 {
		t.Fatalf("应跳过的消息触发了 %d 次 sendmessage, 期望仍为 1（仅入站确认）", n)
	}

	// agent 正常推送，带标题栏（仅会话标题时与旧前缀同形），标题独立一行，
	// context_token 用最近入站的。
	// 出站已改走异步串行队列：FlushForTest 等队列排空后再断言。
	b.DeliverHubMessage(hub.Message{Kind: "agent", SessionTitle: "标题", Text: "hello world"})
	b.FlushForTest()
	sends = rc.sends()
	if len(sends) != 2 {
		t.Fatalf("sendmessage 次数 = %d, 期望 2（入站确认 + agent 推送）", len(sends))
	}
	text, token := sendMsgFields(t, sends[1].body)
	// deliver 阶段统一升级硬换行（行尾两空格，微信 PC 端 Markdown 渲染需要）。
	if text != "【标题】  \nhello world" {
		t.Errorf("出站文本 = %q, 期望 【标题】  \\nhello world", text)
	}
	if token != "ct-9" {
		t.Errorf("context_token = %q, 期望 ct-9", token)
	}
}

// 标题栏归因：项目名（工作区基名）· agent 展示名 · 会话标题；展示名查不到
// 回落工具 ID；Workspace/Tool/SessionTitle 全空时不加标题栏。
func TestBridgeDeliverHubMessageTitleBar(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.SetToolName(func(id string) string {
		if id == "claude" {
			return "Claude Code"
		}
		return ""
	})
	b.HandleInbound("u1", "ct-1", "你好") // 建立发送上下文
	b.FlushForTest()

	b.DeliverHubMessage(hub.Message{Kind: "agent", Workspace: `D:\ws\proj`, Tool: "claude",
		SessionTitle: "标题", Text: "正文"})
	// 展示名查不到回落工具 ID。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Tool: "codex", Text: "无标题"})
	// 全空：不加标题栏。
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "裸文本"})
	b.FlushForTest()
	sends := rc.sends()
	if len(sends) != 4 {
		t.Fatalf("sendmessage 次数 = %d, 期望 4（入站确认 + 3 条推送）", len(sends))
	}
	if text, _ := sendMsgFields(t, sends[1].body); text != "【proj · Claude Code · 标题】  \n正文" {
		t.Errorf("出站文本 = %q, 期望 【proj · Claude Code · 标题】  \\n正文", text)
	}
	if text, _ := sendMsgFields(t, sends[2].body); text != "【codex】  \n无标题" {
		t.Errorf("出站文本 = %q, 期望 【codex】  \\n无标题", text)
	}
	if text, _ := sendMsgFields(t, sends[3].body); text != "裸文本" {
		t.Errorf("出站文本 = %q, 期望 裸文本", text)
	}
}

// 回归：DeliverHubMessage 不经 FlushForTest 也要能送达——入队时惰性启动出站
// worker。生产路径没人调 FlushForTest，worker 不启动则队列永远无人消费，
// agent 回复全部滞留（真机「命令回复正常、agent 回复收不到」的根因）。
func TestBridgeOutboundWorkerLazyStart(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好") // 建立发送上下文（同步发一条入站确认）
	if n := len(rc.sends()); n != 1 {
		t.Fatalf("入站确认 sendmessage 次数 = %d, 期望 1", n)
	}
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "异步推送"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sends := rc.sends()
		if len(sends) >= 2 {
			text, token := sendMsgFields(t, sends[1].body)
			if text != "异步推送" {
				t.Errorf("出站文本 = %q, 期望 异步推送", text)
			}
			if token != "ct-1" {
				t.Errorf("context_token = %q, 期望 ct-1", token)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("2s 内未收到异步推送，sendmessage 次数 = %d（出站 worker 未消费队列？）", len(rc.sends()))
}

func TestBridgeDeliverHardBreaks(t *testing.T) {
	// 出站统一套用 Markdown 硬换行：PC 端按 Markdown 渲染会把单个 \n
	// 折叠成空格（真机证实），行尾两空格两端都能换行。
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-9", "你好") // call 1 是入站确认

	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "第一行\n第二行\n\n第三段"})
	b.FlushForTest()
	sends := rc.sends()
	if len(sends) != 2 {
		t.Fatalf("sendmessage 次数 = %d, 期望 2（入站确认 + agent 推送）", len(sends))
	}
	text, _ := sendMsgFields(t, sends[1].body)
	if text != "第一行  \n第二行\n\n第三段" {
		t.Errorf("出站文本 = %q, 期望 %q", text, "第一行  \n第二行\n\n第三段")
	}
}

func TestBridgePendingOnSessionExpired(t *testing.T) {
	rc := &recorder{sendResp: func(call int) (int, string) {
		if call == 2 { // call 1 是入站确认；首条出站 sendmessage（call 2）返回 -14
			return 200, `{"errcode":-14,"errmsg":"expired"}`
		}
		return 200, "{}"
	}}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好") // Send 成功，入站确认走 sendmessage（call 1），ctx 已记录

	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "待发内容"})
	b.FlushForTest()
	if b.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, 期望 1", b.PendingCount())
	}
	if b.Undelivered() != 0 {
		t.Errorf("排队中不应计入失败: Undelivered = %d", b.Undelivered())
	}

	// 服务端恢复后再入站 → pending 自动补发。
	b.HandleInbound("u1", "ct-2", "再来一条")
	sends := rc.sends()
	if len(sends) != 4 { // 入站确认、失败的待发内容、补发、第二条入站确认
		t.Fatalf("sendmessage 次数 = %d, 期望 4", len(sends))
	}
	text, token := sendMsgFields(t, sends[2].body)
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
			if call == 2 || call == 3 { // call 1 是入站确认（成功）；出站重试三连
				return 500, "{}"
			}
			return 200, "{}"
		}}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.HandleInbound("u1", "ct-1", "你好")
		b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "重试内容"})
		b.FlushForTest()
		if n := len(rc.sends()); n != 4 {
			t.Errorf("sendmessage 次数 = %d, 期望 4（入站确认 + 重试 3 次）", n)
		}
		if b.Undelivered() != 0 {
			t.Errorf("重试成功后 Undelivered = %d, 期望 0", b.Undelivered())
		}
	})

	t.Run("三次全失败计一次", func(t *testing.T) {
		rc := &recorder{sendResp: func(int) (int, string) { return 500, "{}" }}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.HandleInbound("u1", "ct-1", "你好") // 入站确认三次全失败，undelivered = 1
		b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "失败内容"})
		b.FlushForTest()
		if n := len(rc.sends()); n != 6 {
			t.Errorf("sendmessage 次数 = %d, 期望 6（确认与推送各重试 3 次）", n)
		}
		if b.Undelivered() != 2 {
			t.Errorf("Undelivered = %d, 期望 2（确认与推送各计一次）", b.Undelivered())
		}
		if b.PendingCount() != 0 {
			t.Errorf("普通失败不应入 pending: PendingCount = %d", b.PendingCount())
		}
	})
}

// flushPending 只在「本轮实际成功补发 ≥1 条」时才清零 undelivered：
// pending 空时普通发送失败的计数不得被下一条入站消息误清。
func TestBridgeUndeliveredNotClearedWhenPendingEmpty(t *testing.T) {
	rc := &recorder{sendResp: func(call int) (int, string) {
		// 出站 sendmessage 全部失败（call 1/2/3 = 重试三连）。
		return 500, "{}"
	}}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好") // 入站确认三连失败，undelivered = 1
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: "失败内容"})
	b.FlushForTest()
	if got := b.Undelivered(); got != 2 {
		t.Fatalf("发送失败后 Undelivered = %d, 期望 2（确认与推送各计一次）", got)
	}
	// pending 为空的入站消息不得清零计数。
	b.HandleInbound("u1", "ct-2", "再来一条")
	if got := b.Undelivered(); got != 3 {
		t.Fatalf("pending 空时入站不得清零: Undelivered = %d, 期望 3", got)
	}
}

func TestBridgeTruncate(t *testing.T) {
	rc := &recorder{}
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.HandleInbound("u1", "ct-1", "你好") // 入站确认（sends[0]）
	b.DeliverHubMessage(hub.Message{Kind: "agent", Text: strings.Repeat("字", 2000)})
	b.FlushForTest()
	sends := rc.sends()
	if len(sends) != 2 {
		t.Fatalf("sendmessage 次数 = %d, 期望 2（入站确认 + 推送）", len(sends))
	}
	text, _ := sendMsgFields(t, sends[1].body)
	runes := []rune(text)
	if len(runes) > 1510 {
		t.Errorf("截断后 rune 数 = %d, 期望 ≤1510", len(runes))
	}
	if !strings.Contains(text, "完整内容见桌面端") {
		t.Errorf("截断文本缺少提示后缀: %q", text[len(text)-40:])
	}
}

// —— 附件队列与合并投递 ——

// newAttachBridge 构造支持附件的桥：临时落盘目录 + MediaBase 指向测试服务 +
// /download 路由（返回 plain 的 ECB 密文；downloadStatus 非 200 模拟下载失败）。
// 返回 recorder 供断言 sendmessage 内容。
func newAttachBridge(t *testing.T, ex hub.Executor, plain []byte, downloadStatus int) (*Bridge, *recorder) {
	t.Helper()
	cipher := ecbEncrypt(t, []byte("0123456789abcdef"), plain)
	rc := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			if downloadStatus != 200 {
				w.WriteHeader(downloadStatus)
				return
			}
			w.Write(cipher)
			return
		}
		rc.handler(t)(w, r)
	}))
	t.Cleanup(srv.Close)
	b := NewBridge(NewClient("tok", srv.URL), ex, BridgeConfig{Wxid: "u1", Language: "zh-CN"})
	b.sleep = func(time.Duration) {}
	b.client.MediaBase = srv.URL
	b.filesDir = t.TempDir()
	return b, rc
}

// fileKeyB64 是测试文件附件的密钥（base64(hex 32 字符) 形态，与密文 key 对应）。
var fileKeyB64 = base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString([]byte("0123456789abcdef"))))

// attachMsg 构造一个文件附件消息。
func attachMsg(token, name string) InboundMsg {
	return InboundMsg{
		FromUserID:   "u1",
		ContextToken: token,
		Attachments: []InboundAttachment{{
			Kind: "file", Name: name, EncryptQueryParam: "eqp-1",
			AESKey: fileKeyB64, MD5: "",
		}},
	}
}

func TestBridgeAttachmentMergeWithText(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("附件内容"), 200)

	// 附件先到：入队 + 回执，不投 executor。
	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "需求.pdf"))
	if got := len(ex.sentTexts); got != 0 {
		t.Fatalf("附件到达不应投 executor, 实际 %d 条", got)
	}
	if n := b.QueuedFiles(); n != 1 {
		t.Fatalf("队列 = %d, 期望 1", n)
	}
	sends := rc.sends()
	if len(sends) == 0 || !strings.Contains(sendText(t, sends[0].body), "已收到") {
		t.Fatalf("附件回执缺失: %v", sends)
	}

	// 文本到达：合并投递，队列清空。
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "帮我修复登录"})
	if len(ex.sentTexts) != 1 {
		t.Fatalf("executor 应收到 1 条合并文本, 实际 %d", len(ex.sentTexts))
	}
	merged := ex.sentTexts[0]
	if !strings.Contains(merged, "[附件] 需求.pdf 已保存至 ") {
		t.Errorf("合并文本缺少附件说明行: %q", merged)
	}
	if !strings.Contains(merged, "帮我修复登录") {
		t.Errorf("合并文本缺少用户文字: %q", merged)
	}
	if b.QueuedFiles() != 0 {
		t.Errorf("队列未清空: %d", b.QueuedFiles())
	}
	// 附件说明行里的路径真实存在且内容解密正确（说明行后跟空行与用户文字）。
	i := strings.Index(merged, "已保存至 ")
	rest := merged[i+len("已保存至 "):]
	path := rest[:strings.IndexAny(rest, "\n")]
	if data, err := os.ReadFile(path); err != nil || string(data) != "附件内容" {
		t.Errorf("落盘文件不可读或内容不符: %q %v", path, err)
	}
}

func TestBridgeAttachmentAfterTextQueues(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, _ := newAttachBridge(t, ex, []byte("内容"), 200)

	// 文字先到：立即转发（无附件说明）。
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-1", Text: "第一段需求"})
	if len(ex.sentTexts) != 1 || strings.Contains(ex.sentTexts[0], "[附件]") {
		t.Fatalf("纯文字应立即原样转发: %v", ex.sentTexts)
	}
	// 附件后到：进队列等下一条文字，不独立转发。
	b.HandleInboundMsg(context.Background(), attachMsg("ct-2", "b.pdf"))
	if len(ex.sentTexts) != 1 {
		t.Fatalf("后到附件不应独立转发: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 1 {
		t.Fatalf("队列 = %d, 期望 1", b.QueuedFiles())
	}
	// 第二条文字：合并。
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-3", Text: "补充说明"})
	if len(ex.sentTexts) != 2 {
		t.Fatalf("合并投递缺失: %v", ex.sentTexts)
	}
	if !strings.Contains(ex.sentTexts[1], "[附件]") || !strings.Contains(ex.sentTexts[1], "补充说明") {
		t.Errorf("第二条应含附件说明+文字: %q", ex.sentTexts[1])
	}
}

func TestBridgeAttachmentCommandKeepsQueue(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "a.pdf"))
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "#l"})
	// #l 是命令：照常执行（有回复），队列不动、executor 无投递。
	if len(ex.sentTexts) != 0 {
		t.Fatalf("命令不应携带附件投递: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 1 {
		t.Fatalf("命令后队列应保留, 实际 %d", b.QueuedFiles())
	}
	sends := rc.sends()
	// send[0]=附件回执，send[1]=#l 的命令回复（fake 无会话 → 暂无会话）。
	if len(sends) < 2 || !strings.Contains(sendText(t, sends[1].body), "暂无会话") {
		t.Fatalf("#l 回复缺失: %v", sends)
	}
}

func TestBridgeAttachmentDownloadFail(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 403)

	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "a.pdf"))
	if b.QueuedFiles() != 0 {
		t.Fatalf("下载失败不应入队: %d", b.QueuedFiles())
	}
	sends := rc.sends()
	if len(sends) == 0 || !strings.Contains(sendText(t, sends[0].body), "获取失败") {
		t.Fatalf("失败回执缺失: %v", sends)
	}
	if len(ex.sentTexts) != 0 {
		t.Fatalf("失败附件不应投递: %v", ex.sentTexts)
	}
}

func TestBridgeAttachmentMultiOrder(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "需求.pdf"))
	b.HandleInboundMsg(context.Background(), attachMsg("ct-2", "b.pdf"))
	if b.QueuedFiles() != 2 {
		t.Fatalf("队列 = %d, 期望 2", b.QueuedFiles())
	}
	sends := rc.sends()
	if len(sends) < 2 || !strings.Contains(sendText(t, sends[1].body), "共 2 个") {
		t.Errorf("第二个附件回执应报共 2 个: %v", sends)
	}
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-3", Text: "需求正文"})
	if len(ex.sentTexts) != 1 {
		t.Fatalf("合并投递缺失: %v", ex.sentTexts)
	}
	if n := strings.Count(ex.sentTexts[0], "[附件]"); n != 2 {
		t.Errorf("合并文本应含 2 个附件说明行, 实际 %d: %q", n, ex.sentTexts[0])
	}
	if strings.Index(ex.sentTexts[0], "需求.pdf") > strings.Index(ex.sentTexts[0], "b.pdf") {
		t.Errorf("附件说明行应按到达序: %q", ex.sentTexts[0])
	}
}

func TestBridgeStatusQueuedNote(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "a.pdf"))
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "#st"})
	sends := rc.sends()
	// send[0]=附件回执, send[1]=#st 回复, send[2]=待转交尾注。
	if len(sends) < 3 || !strings.Contains(sendText(t, sends[2].body), "待转交附件 1 个") {
		t.Fatalf("#st 待转交尾注缺失: %v", sends)
	}
}

// sendText 只取 sendmessage 请求体里的文本。
func sendText(t *testing.T, body map[string]any) string {
	t.Helper()
	text, _ := sendMsgFields(t, body)
	return text
}

// —— 引用归因映射 ——

func TestBridgeRememberSvrID(t *testing.T) {
	t.Run("出站成功记录映射", func(t *testing.T) {
		rc := &recorder{sendResp: func(int) (int, string) { return 200, `{"svr_id":"9001"}` }}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.HandleInbound("u1", "ct-1", "你好") // 入站确认（无 chatID，不记录）
		b.DeliverHubMessage(hub.Message{Kind: "agent", ChatID: "c1", SessionTitle: "会话A", Text: "回复"})
		b.FlushForTest()
		b.mu.Lock()
		got := b.svrMap["9001"]
		b.mu.Unlock()
		if got != "c1" {
			t.Fatalf("svrMap[9001] = %q, 期望 c1", got)
		}
	})
	t.Run("响应无 svr_id 不记录不报错", func(t *testing.T) {
		rc := &recorder{}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.DeliverHubMessage(hub.Message{Kind: "agent", ChatID: "c1", Text: "回复"})
		b.FlushForTest()
		b.mu.Lock()
		n := len(b.svrMap)
		b.mu.Unlock()
		if n != 0 {
			t.Fatalf("svrMap 应为空, 实际 %d 条", n)
		}
	})
	t.Run("超上限丢最旧", func(t *testing.T) {
		rc := &recorder{}
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		for i := 0; i < svrMapCap+10; i++ {
			b.rememberSvrID(fmt.Sprintf("svr-%d", i), "c1")
		}
		b.mu.Lock()
		_, hasOld := b.svrMap["svr-0"]
		_, hasNew := b.svrMap[fmt.Sprintf("svr-%d", svrMapCap+9)]
		b.mu.Unlock()
		if hasOld {
			t.Error("最旧映射应被淘汰")
		}
		if !hasNew {
			t.Error("最新映射应保留")
		}
	})
}

func TestQuoteTitle(t *testing.T) {
	for in, want := range map[string]string{
		"【会话A】回复正文":   "会话A",
		"  【带空格标题】正文": "带空格标题",
		"没有前缀的普通文本":   "",
		"【未闭合的标题":     "",
		"【外层【内层】】正文":  "外层【内层",
	} {
		if got := quoteTitle(in); got != want {
			t.Errorf("quoteTitle(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestBridgeResolveQuote(t *testing.T) {
	ex := &fakeExec{sessions: []hub.SessionRef{
		{ChatID: "c1", Title: "会话A"},
		{ChatID: "", SessionID: "s2", Title: "未激活会话"}, // 未激活：ChatID 空，标题匹配应跳过
	}}
	rc := &recorder{}
	b := newBridge(t, "u1", ex, rc.handler(t))
	b.rememberSvrID("9001", "c1")

	t.Run("svr_id 精确命中", func(t *testing.T) {
		if got := b.resolveQuote(&RefMsg{SvrID: "9001", Text: "【不匹配的标题】x"}); got != "c1" {
			t.Errorf("resolveQuote = %q, 期望 c1（svr_id 优先）", got)
		}
	})
	t.Run("标题兜底命中", func(t *testing.T) {
		if got := b.resolveQuote(&RefMsg{SvrID: "unknown", Text: "【会话A】回复正文"}); got != "c1" {
			t.Errorf("resolveQuote = %q, 期望 c1（标题匹配）", got)
		}
	})
	t.Run("未激活会话不参与标题匹配", func(t *testing.T) {
		if got := b.resolveQuote(&RefMsg{SvrID: "unknown", Text: "【未激活会话】正文"}); got != "" {
			t.Errorf("resolveQuote = %q, 期望空（未激活跳过）", got)
		}
	})
	t.Run("双不命中返回空", func(t *testing.T) {
		if got := b.resolveQuote(&RefMsg{SvrID: "x", Text: "普通引用文本"}); got != "" {
			t.Errorf("resolveQuote = %q, 期望空", got)
		}
		if got := b.resolveQuote(nil); got != "" {
			t.Errorf("resolveQuote(nil) = %q, 期望空", got)
		}
	})
}

// —— 引用回复路由 ——

// quoteMsg 构造一条引用回复入站消息。
func quoteMsg(token, svrID, quoteText, text string) InboundMsg {
	return InboundMsg{FromUserID: "u1", ContextToken: token, Text: text, Ref: &RefMsg{SvrID: svrID, Text: quoteText}}
}

func TestBridgeQuoteReplyRouting(t *testing.T) {
	sessions := []hub.SessionRef{
		{ChatID: "c1", Title: "会话A"},
		{ChatID: "c2", Title: "会话B"},
	}

	t.Run("svr_id 命中切会话投递", func(t *testing.T) {
		rc := &recorder{}
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c2", Title: "会话B"}}
		b := newBridge(t, "u1", ex, rc.handler(t))
		b.rememberSvrID("9001", "c1")
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "9001", "【会话A】旧回复", "继续这个"))
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "继续这个" {
			t.Fatalf("应投给引用会话: %v", ex.sentTexts)
		}
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c1" {
			t.Errorf("应切换到 c1: %v", ex.switchCalls)
		}
		// ack 归因前缀应为切换后的新会话标题（用户据它确认切换生效）。
		sends := rc.sends()
		if len(sends) == 0 {
			t.Fatal("应有 ack 回执")
		}
		if ack := sendText(t, sends[0].body); !strings.Contains(ack, "已发送给 会话A") && !strings.Contains(ack, "会话A") {
			t.Errorf("ack 应带新会话【会话A】归因: %q", ack)
		}
	})

	t.Run("标题兜底命中", func(t *testing.T) {
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c2"}}
		b := newBridge(t, "u1", ex, (&recorder{}).handler(t))
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "unknown", "【会话B】旧回复", "看一下"))
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "看一下" {
			t.Fatalf("应按标题投给会话B: %v", ex.sentTexts)
		}
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c2" {
			t.Errorf("应切换到 c2: %v", ex.switchCalls)
		}
	})

	t.Run("双不命中回落当前会话", func(t *testing.T) {
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c2"}}
		b := newBridge(t, "u1", ex, (&recorder{}).handler(t))
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "x", "普通引用无前缀", "普通消息"))
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "普通消息" {
			t.Fatalf("应回落当前会话: %v", ex.sentTexts)
		}
		if len(ex.switchCalls) != 0 {
			t.Errorf("回落不应切换: %v", ex.switchCalls)
		}
	})

	t.Run("引用+命令走普通命令路径", func(t *testing.T) {
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, (&recorder{}).handler(t))
		b.rememberSvrID("9001", "c2")
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "9001", "【会话B】旧回复", "#l"))
		if len(ex.sentTexts) != 0 {
			t.Fatalf("#l 是命令不应投递文本: %v", ex.sentTexts)
		}
		if len(ex.switchCalls) != 0 {
			t.Errorf("命令不应触发切换: %v", ex.switchCalls)
		}
	})

	t.Run("引用+纯数字走编号切换", func(t *testing.T) {
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
		b := newBridge(t, "u1", ex, (&recorder{}).handler(t))
		b.rememberSvrID("9001", "c2")
		// 先 #l 建立编号映射（1 → c1），再引用回复纯数字 1。
		b.HandleInbound("u1", "ct-0", "#l")
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "9001", "【会话B】旧回复", "1"))
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c1" {
			t.Fatalf("纯数字应走编号切换到 c1: %v", ex.switchCalls)
		}
		if len(ex.sentTexts) != 0 {
			t.Errorf("编号切换不应投递文本: %v", ex.sentTexts)
		}
	})

	t.Run("引用+附件队列照常合并", func(t *testing.T) {
		ex := &fakeExec{sessions: sessions, hasCur: true, current: hub.SessionRef{ChatID: "c2"}}
		cipher := ecbEncrypt(t, []byte("0123456789abcdef"), []byte("内容"))
		rc := &recorder{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/download" {
				w.Write(cipher)
				return
			}
			rc.handler(t)(w, r)
		}))
		defer srv.Close()
		bb := NewBridge(NewClient("tok", srv.URL), ex, BridgeConfig{Wxid: "u1", Language: "zh-CN"})
		bb.sleep = func(time.Duration) {}
		bb.client.MediaBase = srv.URL
		bb.filesDir = t.TempDir()
		bb.rememberSvrID("9001", "c1")

		bb.HandleInboundMsg(context.Background(), attachMsg("ct-0", "a.pdf"))
		bb.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "9001", "【会话A】旧回复", "看这个"))
		if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件] a.pdf") || !strings.Contains(ex.sentTexts[0], "看这个") {
			t.Fatalf("引用回复应与附件队列合并: %v", ex.sentTexts)
		}
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c1" {
			t.Errorf("合并投递同样应切换: %v", ex.switchCalls)
		}
		if bb.QueuedFiles() != 0 {
			t.Errorf("队列应清空: %d", bb.QueuedFiles())
		}
	})
}

func TestBridgeAttachmentAndTextSameMessage(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	// 同条消息同时带附件与文字：附件直接随本轮文字合并投递，不发附件回执。
	m := attachMsg("ct-1", "需求.pdf")
	m.Text = "看这个文件"
	b.HandleInboundMsg(context.Background(), m)
	if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件]") || !strings.Contains(ex.sentTexts[0], "看这个文件") {
		t.Fatalf("同条消息应合并投递: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 0 {
		t.Fatalf("队列应已清空: %d", b.QueuedFiles())
	}
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("同条消息只应有 ack 一条 sendmessage: %v", sends)
	}
	if strings.Contains(sendText(t, sends[0].body), "已收到") {
		t.Errorf("合并投递后不应再发附件回执")
	}
}

// —— 语音转写 ——

// voiceMsg 构造一条纯语音消息（无服务端转写；媒体下载指向测试服务）。
func voiceMsg(token string) InboundMsg {
	return InboundMsg{
		FromUserID:   "u1",
		ContextToken: token,
		Voice: &InboundAttachment{
			Kind: "voice", Name: "voice.silk", EncryptQueryParam: "eqp-1", AESKey: fileKeyB64,
		},
	}
}

func TestBridgeVoiceTranscribedForward(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	var gotWAV []byte
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		gotWAV = wav
		return "语音说的需求", nil
	})

	b.HandleInboundMsg(context.Background(), voiceMsg("ct-1"))
	// 语音落盘入队后与转写文本合并投递：附件说明行 + 转写文本，一条 ack。
	if len(ex.sentTexts) != 1 {
		t.Fatalf("转写文本应合并投递, 实际 %v", ex.sentTexts)
	}
	merged := ex.sentTexts[0]
	if !strings.Contains(merged, "[附件] voice.wav 已保存至 ") || !strings.Contains(merged, "语音说的需求") {
		t.Fatalf("合并文本应含语音文件说明行与转写文本: %q", merged)
	}
	if !bytes.HasPrefix(gotWAV, []byte("RIFF")) {
		t.Errorf("transcriber 收到的不是 WAV: % x", gotWAV[:min(4, len(gotWAV))])
	}
	if b.QueuedFiles() != 0 {
		t.Errorf("队列应已清空: %d", b.QueuedFiles())
	}
	// 落盘语音文件可读且是合法 WAV（说明行后跟空行与转写文本）。
	i := strings.Index(merged, "已保存至 ")
	rest := merged[i+len("已保存至 "):]
	path := rest[:strings.IndexAny(rest, "\n")]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("语音文件未落盘: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) {
		t.Errorf("落盘文件不是 WAV: % x", data[:min(4, len(data))])
	}
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("sendmessage 次数 = %d, 期望 1（ack）", len(sends))
	}
}

func TestBridgeVoiceMergesWithQueue(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		return "看一下刚发的文件", nil
	})

	// 附件先入队（回执 send[0]），语音转写文本与其合并投递。
	b.HandleInboundMsg(context.Background(), attachMsg("ct-1", "a.pdf"))
	b.HandleInboundMsg(context.Background(), voiceMsg("ct-2"))
	if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件] a.pdf") ||
		!strings.Contains(ex.sentTexts[0], "看一下刚发的文件") {
		t.Fatalf("语音转写应与附件队列合并: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 0 {
		t.Fatalf("队列应清空: %d", b.QueuedFiles())
	}
	sends := rc.sends()
	if len(sends) != 2 {
		t.Fatalf("sendmessage 次数 = %d, 期望 2（附件回执 + ack）", len(sends))
	}
	if strings.Contains(sendText(t, sends[1].body), "转写") {
		t.Errorf("合并成功不应再发语音回执")
	}
}

func TestBridgeVoiceUnconfigured(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	// 不注入 transcriber：语音文件仍落盘入队（转交不依赖 ASR），回执
	//「未配置」，不投递。
	b.HandleInboundMsg(context.Background(), voiceMsg("ct-1"))
	if len(ex.sentTexts) != 0 {
		t.Fatalf("未配置不应投递: %v", ex.sentTexts)
	}
	if n := b.QueuedFiles(); n != 1 {
		t.Fatalf("语音文件应入队待转交, 队列 = %d", n)
	}
	sends := rc.sends()
	if len(sends) == 0 || !strings.Contains(sendText(t, sends[0].body), "未配置") {
		t.Fatalf("未配置回执缺失: %v", sends)
	}
}

func TestBridgeVoiceTranscribeFail(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		return "", context.DeadlineExceeded
	})

	b.HandleInboundMsg(context.Background(), voiceMsg("ct-1"))
	if len(ex.sentTexts) != 0 {
		t.Fatalf("转写失败不应投递: %v", ex.sentTexts)
	}
	if n := b.QueuedFiles(); n != 1 {
		t.Fatalf("转写失败后语音文件仍应入队待转交, 队列 = %d", n)
	}
	sends := rc.sends()
	if len(sends) == 0 || !strings.Contains(sendText(t, sends[0].body), "转写失败") {
		t.Fatalf("失败回执缺失: %v", sends)
	}
}

func TestBridgeVoiceAudioUnsupported(t *testing.T) {
	// 对话模型不支持音频输入（常态而非异常）：回执走 voice.unsupported
	// 指引文案，不透传底层网关报错，也不投递。
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		return "", fmt.Errorf("%w: 状态码 400", ErrVoiceUnsupported)
	})

	b.HandleInboundMsg(context.Background(), voiceMsg("ct-1"))
	if len(ex.sentTexts) != 0 {
		t.Fatalf("不支持音频不应投递: %v", ex.sentTexts)
	}
	sends := rc.sends()
	if len(sends) == 0 {
		t.Fatal("应有回执")
	}
	text := sendText(t, sends[0].body)
	if !strings.Contains(text, "不支持") && !strings.Contains(text, "not support") {
		t.Fatalf("回执应为不支持音频指引文案: %v", text)
	}
	if strings.Contains(text, "状态码 400") {
		t.Fatalf("回执不应透传底层报错: %v", text)
	}
}

func TestBridgeVoiceServerTextSkipsASR(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, _ := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		t.Error("有服务端转写时不应调用模型 ASR")
		return "", nil
	})

	m := voiceMsg("ct-1")
	m.Text = "服务端转写内容"
	b.HandleInboundMsg(context.Background(), m)
	// 服务端转写直通，且语音文件同样落盘随行转交。
	if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件] voice.wav 已保存至 ") ||
		!strings.Contains(ex.sentTexts[0], "服务端转写内容") {
		t.Fatalf("服务端转写应直通并携带语音文件: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 0 {
		t.Fatalf("队列应清空: %d", b.QueuedFiles())
	}
}

func TestBridgeVoiceEmptyTranscriptKeepsFile(t *testing.T) {
	// 转写结果为空：语音文件仍入队待下一条文字转交，回执「转写结果为空」。
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	pcm := bytes.Repeat([]byte{0x10, 0x02}, 1200)
	b, rc := newAttachBridge(t, ex, encodeSilk(t, pcm, true), 200)
	b.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) {
		return "", nil
	})

	b.HandleInboundMsg(context.Background(), voiceMsg("ct-1"))
	if len(ex.sentTexts) != 0 {
		t.Fatalf("空转写不应投递: %v", ex.sentTexts)
	}
	if n := b.QueuedFiles(); n != 1 {
		t.Fatalf("语音文件应入队待转交, 队列 = %d", n)
	}
	// 下一条文字合并转交语音文件。
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "听一下"})
	if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件] voice.wav 已保存至 ") ||
		!strings.Contains(ex.sentTexts[0], "听一下") {
		t.Fatalf("下一条文字应合并转交语音文件: %v", ex.sentTexts)
	}
	sends := rc.sends()
	if len(sends) != 2 {
		t.Fatalf("sendmessage 次数 = %d, 期望 2（空转写回执 + ack）", len(sends))
	}
}
