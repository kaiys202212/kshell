package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
)

// TestSendChatPromptDirectSkipsHubRoute 验证 ACP 直投入口完全绕过 AgentHub 的
// 工具化路由：即便路由模式是 force_auto（最激进）、模型配置就绪，LLM 端点也
// 一次都不该被调用；用户消息立即落盘并推送 hub:changed。
func TestSendChatPromptDirectSkipsHubRoute(t *testing.T) {
	mock := &hubLLMMock{bodies: []string{`"不该被调用"`}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	setHubModel(a, srv.URL, "k-test", "test-model")
	a.opts.Config.AgentHub.RouteMode = config.RouteModeForceAuto

	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	// ack 回调必须接线：直投走的仍是 sendAndRecord，投递成功后应补 ack_delivered
	m.SetOnHubCallbacks(a.onHubQueued, a.onHubDelivered)
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}

	if err := a.SendChatPromptDirect(info.ID, "直投测试"); err != nil {
		t.Fatal(err)
	}

	// LLM 一次都不该被调用：直投不经过工具化路由
	mock.mu.Lock()
	calls := mock.calls
	mock.mu.Unlock()
	if calls != 0 {
		t.Fatalf("直投不应触达 LLM，实际调用 %d 次", calls)
	}

	u := waitHubText(t, store, "直投测试")
	if u.Kind != "user" || !strings.Contains(u.Text, "直投测试") {
		t.Fatalf("应落盘 user 消息: %+v", u)
	}
	// 直投必须仍走完整投递链：ack 卡片与 queued 清空都不能被绕过
	waitHubEvent(t, store, "ui.hub.ack_delivered")
	for _, mm := range store.List() {
		if mm.ID == u.ID && mm.Status != "" {
			t.Fatalf("投递后 status 应清空: %+v", mm)
		}
	}
	if rec.count("hub:changed") == 0 {
		t.Fatal("应 emit hub:changed")
	}
}

// TestSendChatPromptDirectReactivatesAndDelivers 验证直投共用 ensureChatAlive：
// 会话进程已退出时，用前端持有的旧 ID 直投应触发重建并把消息投给新连接（别名链
// 解析 + 重建），后续仍按旧 ID 可继续投递。
func TestSendChatPromptDirectReactivatesAndDelivers(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	if err := app.SendChatPromptDirect(oldID, "继续修"); err != nil {
		t.Fatalf("SendChatPromptDirect: %v", err)
	}
	if b.count() != 2 {
		t.Fatalf("直投应触发重建, got %d 次连接", b.count())
	}
	waitPromptText(t, b.connAt(1), "继续修")

	// 第二条仍用旧 ID：别名解析应继续投到重建后的连接
	if err := app.SendChatPromptDirect(oldID, "再查一下"); err != nil {
		t.Fatalf("第二条 SendChatPromptDirect: %v", err)
	}
	waitPromptText(t, b.connAt(1), "再查一下")
}


// TestSendChatPromptStillRoutesHub 守住 AgentHub 面板语义：SendChatPrompt 在
// force_auto 下仍应走工具化路由——LLM 被调用，且模型回复作为 agent 消息落盘
//（证明路由真正消费了本轮输入，而不是只触达端点后退化成直连）。
func TestSendChatPromptStillRoutesHub(t *testing.T) {
	mock := &hubLLMMock{bodies: []string{`"好的。"`}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	a, store, rec := newBridgeTestApp(t)
	setHubModel(a, srv.URL, "k-test", "test-model")
	a.opts.Config.AgentHub.RouteMode = config.RouteModeForceAuto
	if _, err := a.getRequirementStore().Add(`D:\ws-a`, "修复登录", "high", nil); err != nil {
		t.Fatal(err)
	}

	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}

	if err := a.SendChatPrompt(info.ID, "列出需求"); err != nil {
		t.Fatal(err)
	}

	// 工具化路径是异步收尾的：等模型最终回复落盘（LLM 调用 + 结果消费都成立）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, mm := range store.List() {
			if mm.Kind == "agent" && mm.Event == "" && strings.Contains(mm.Text, "好的。") {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	mock.mu.Lock()
	calls := mock.calls
	mock.mu.Unlock()
	t.Fatalf("SendChatPrompt 应仍走 AgentHub 工具化路由并落模型回复（LLM 调用 %d 次）", calls)
}

// TestSendChatPromptDirectIgnoresFollowCommand 验证直投不做 #f 指令解析：
// 即使文本以 "#f " 开头，也原样投给入参指定的会话，而不是被转到「最近一条
// agent 消息归属」的会话。
func TestSendChatPromptDirectIgnoresFollowCommand(t *testing.T) {
	a, store, rec := newBridgeTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	chatA, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "A", ToolID: "cursor",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	chatB, err := m.Open("session:s2", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w2", Title: "B", ToolID: "claude",
	}, chat.Spec{Path: "y"}, "s2")
	if err != nil {
		t.Fatal(err)
	}
	// 最近一条 agent 消息归属 A：若直投误做 #f 解析，消息就会被投到 A
	if err := store.Append(hub.Message{
		Kind: "agent", ChatID: chatA.ID, Tool: "cursor", SessionTitle: "A", Text: "我在修",
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.SendChatPromptDirect(chatB.ID, "#f 直投"); err != nil {
		t.Fatal(err)
	}

	u := waitHubText(t, store, "#f 直投")
	if u.ChatID != chatB.ID {
		t.Fatalf("直投消息应归因入参会话 B(%s)，实际 %s: %+v", chatB.ID, u.ChatID, u)
	}
	if !strings.Contains(u.Text, "#f 直投") {
		t.Fatalf("文本应原样落盘: %q", u.Text)
	}
}
