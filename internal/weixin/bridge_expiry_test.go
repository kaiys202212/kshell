package weixin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/hub"
)

// hasExpiredReceipt 扫描出站发送里是否含附件超时回执文案。
func hasExpiredReceipt(t *testing.T, texts []string) bool {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, "超时忽略") {
			return true
		}
	}
	return false
}

// expireQueue 把队列整体拨到过期（测试直接操纵时间戳，避免真实等待）。
func expireQueue(b *Bridge, d time.Duration) {
	b.mu.Lock()
	b.queueAt = b.now().Add(-d)
	b.mu.Unlock()
}

// 只发图片没有配文字：超过 1 分钟后补的文字按普通文本路由（不带附件说明行），
// 附件整批丢弃并回执，不再弹「附件消息需要指定目标会话」待选菜单。
func TestBridgeAttachmentExpiredDropped(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(t.Context(), attachMsg("ct-1", "截图.png"))
	if b.QueuedFiles() != 1 {
		t.Fatalf("附件应入队: %d", b.QueuedFiles())
	}
	expireQueue(b, 61*time.Second)

	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "帮我修复登录"})
	if len(ex.sentTexts) != 1 {
		t.Fatalf("executor 应收到 1 条纯文本, 实际 %v", ex.sentTexts)
	}
	if strings.Contains(ex.sentTexts[0], "[附件]") {
		t.Errorf("过期附件不应随文字合并: %q", ex.sentTexts[0])
	}
	if !strings.Contains(ex.sentTexts[0], "帮我修复登录") {
		t.Errorf("文字应原样转发: %q", ex.sentTexts[0])
	}
	if b.QueuedFiles() != 0 {
		t.Errorf("过期后队列应清空: %d", b.QueuedFiles())
	}
	var texts []string
	for _, s := range rc.sends() {
		texts = append(texts, sendText(t, s.body))
	}
	if !hasExpiredReceipt(t, texts) {
		t.Errorf("缺少超时丢弃回执: %v", texts)
	}
	for _, s := range texts {
		if strings.Contains(s, "请回复编号选择目标会话") {
			t.Errorf("过期后不应再弹待选菜单: %q", s)
		}
	}
}

// 多图以最新一张为基准续期整批：t0 图A、t0+50s 图B、t0+70s 文字，
// 距最早一张已超 1 分钟但整批仍有效，正常合并含 A+B。
func TestBridgeAttachmentExpiryRenewal(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	base := time.Now()
	clock := base
	b.nowFn = func() time.Time { return clock }

	b.HandleInboundMsg(t.Context(), attachMsg("ct-1", "a.pdf"))
	clock = base.Add(50 * time.Second)
	b.HandleInboundMsg(t.Context(), attachMsg("ct-2", "b.pdf"))
	if b.QueuedFiles() != 2 {
		t.Fatalf("两图应都在队列: %d", b.QueuedFiles())
	}
	clock = base.Add(70 * time.Second)

	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-3", Text: "这两张图对比"})
	if len(ex.sentTexts) != 1 {
		t.Fatalf("应合并投递 1 条, 实际 %v", ex.sentTexts)
	}
	if !strings.Contains(ex.sentTexts[0], "[附件] a.pdf") || !strings.Contains(ex.sentTexts[0], "[附件] b.pdf") {
		t.Errorf("整批续期应包含两张图: %q", ex.sentTexts[0])
	}
	if !strings.Contains(ex.sentTexts[0], "这两张图对比") {
		t.Errorf("缺少用户文字: %q", ex.sentTexts[0])
	}
	for _, s := range rc.sends() {
		if strings.Contains(sendText(t, s.body), "超时忽略") {
			t.Errorf("未过期批不应有超时回执: %q", sendText(t, s.body))
		}
	}
}

// 挂起反问等待回答期间不超时丢弃：用户思考超过 1 分钟后补的文字仍走显式
// 续跑并携带附件（2026-11-19 续跑流保护），不发超时回执。
func TestBridgeAttachmentExpiryKeptWhileAskPending(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "legacy", Title: "无关会话"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "legacy", Title: "无关会话"},
	}
	b, rc := newRouteBridge(t, ex)
	b.SetRouteGate(func() bool { return true })
	b.SetAskPending(func() bool { return true })
	var resumed []string
	b.SetAskResumer(func(_ context.Context, input string) (ManagerOutcome, bool) {
		resumed = append(resumed, input)
		return ManagerOutcome{Replies: []string{"已按答案继续。"}}, true
	})

	queueAttachment(t, b, "需求.pdf")
	expireQueue(b, 5*time.Minute)

	b.HandleInbound("u1", "ct-1", "选方案 B，按截图样式改")

	if len(resumed) != 1 {
		t.Fatalf("AskResumer 调用次数 = %d, 期望 1 (%v)", len(resumed), resumed)
	}
	if !strings.Contains(resumed[0], "[附件] 需求.pdf") || !strings.Contains(resumed[0], "选方案 B") {
		t.Errorf("续跑输入应含附件说明行与答案: %q", resumed[0])
	}
	if b.QueuedFiles() != 0 {
		t.Errorf("续跑成功后队列应清空: %d", b.QueuedFiles())
	}
	var texts []string
	for _, s := range rc.sends() {
		texts = append(texts, sendText(t, s.body))
	}
	if hasExpiredReceipt(t, texts) {
		t.Errorf("askPending 期间不应有超时回执: %v", texts)
	}
}

// 待选菜单挂起期间不超时丢弃：暂存正文已含附件说明行，此刻丢附件会造成
// 「编号选完后附件没了」的悬空引用；编号回复成功投递后队列照常消费。
func TestBridgeAttachmentExpiryKeptWhilePickPending(t *testing.T) {
	ex := &fakeExec{sessions: []hub.SessionRef{
		{ChatID: "c1", Tool: "claude", Title: "会话A", Workspace: `D:\ws\a`},
	}}
	b, _ := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(t.Context(), attachMsg("ct-1", "a.pdf"))
	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "帮我修复"})
	if b.QueuedFiles() != 1 {
		t.Fatalf("待选阶段队列应保留: %d", b.QueuedFiles())
	}
	expireQueue(b, 5*time.Minute)

	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-3", Text: "2"})
	if len(ex.sentTexts) != 1 || !strings.Contains(ex.sentTexts[0], "[附件] a.pdf") {
		t.Fatalf("回编号应发送含附件说明的暂存正文: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 0 {
		t.Errorf("成功投递后队列应清空: %d", b.QueuedFiles())
	}
}

// 超时后用户先发命令（#l）：命令照常执行，同时收到超时丢弃回执，队列清零。
func TestBridgeExpiredDroppedOnCommand(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.HandleInboundMsg(t.Context(), attachMsg("ct-1", "a.pdf"))
	expireQueue(b, 61*time.Second)

	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-2", Text: "#l"})
	if b.QueuedFiles() != 0 {
		t.Errorf("命令入口也应惰性过期清队列: %d", b.QueuedFiles())
	}
	var texts []string
	for _, s := range rc.sends() {
		texts = append(texts, sendText(t, s.body))
	}
	if !hasExpiredReceipt(t, texts) {
		t.Errorf("命令消息也应收到超时回执: %v", texts)
	}
}

// 预填队列（语音等无独立 ingest 的路径同队列同待遇）：过期后同样丢弃。
func TestBridgePrefilledExpiredQueueDropped(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1"}}
	b, rc := newAttachBridge(t, ex, []byte("内容"), 200)

	b.mu.Lock()
	b.queue = append(b.queue, queuedFile{Name: "voice.wav", Path: t.TempDir() + "/voice.wav"})
	b.mu.Unlock()
	expireQueue(b, 61*time.Second)

	b.HandleInboundMsg(t.Context(), InboundMsg{FromUserID: "u1", ContextToken: "ct-1", Text: "刚才那条语音不要了，看下日志"})
	if b.QueuedFiles() != 0 {
		t.Errorf("预填过期队列应清空: %d", b.QueuedFiles())
	}
	var texts []string
	for _, s := range rc.sends() {
		texts = append(texts, sendText(t, s.body))
	}
	if !hasExpiredReceipt(t, texts) {
		t.Errorf("缺少超时丢弃回执: %v", texts)
	}
}
