package desktop

import (
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
)

// newDirectTestApp 构造直投用例所需的最小 App：Hub 记忆存储 + 事件收集。
// 工具循环下线后不再需要需求库/工作区扫描等工具侧依赖。
func newDirectTestApp(t *testing.T) (*App, *hub.Store, *notifyRecorder) {
	t.Helper()
	store := hub.New(t.TempDir(), 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	rec := &notifyRecorder{}
	app := NewAppWith(Options{Home: t.TempDir(), HubStore: store, Emit: rec.emit})
	return app, store, rec
}

// TestSendChatPromptDirectRecordsImmediately 验证直投入口：跳过一切模型分发，
// 用户消息立即落盘并推送 hub:changed，ack 回调不被绕过。
func TestSendChatPromptDirectRecordsImmediately(t *testing.T) {
	a, store, rec := newDirectTestApp(t)

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

	if err := a.SendChatPromptDirect(info.ID, "直投测试"); err != nil {
		t.Fatal(err)
	}

	u := waitHubText(t, store, "直投测试")
	if u.Kind != "user" {
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

// TestSendChatPromptDirectIgnoresFollowCommand 验证直投不做 #f 指令解析：
// 即使文本以 "#f " 开头，也原样投给入参指定的会话，而不是被转到「最近一条
// agent 消息归属」的会话。
func TestSendChatPromptDirectIgnoresFollowCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
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

// TestSendChatPromptHasNoLLMStep 守住本次修复的核心契约：普通发送入口
// （AgentHub @目标 / 编号选择 / 引用回复都经它）不再有任何模型分发步骤——
// 即便模型配置完全就绪，发送也必须是纯本地投递。
func TestSendChatPromptHasNoLLMStep(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	// 配置一个语法上"就绪"的模型端点：修复前它会触发工具循环；现在不应触达。
	a.opts.Config.Model.Platforms = []config.ModelPlatform{
		{ID: "p-test", OpenAIBaseURL: "http://127.0.0.1:1", APIKey: "k-test"},
	}
	a.opts.Config.Model.ActivePlatformID = "p-test"
	a.opts.Config.Model.ActiveModel = "test-model"

	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m
	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatal(err)
	}

	if err := a.SendChatPrompt(info.ID, "精确投递"); err != nil {
		t.Fatal(err)
	}
	// 同步落盘即可：一旦有 LLM 往返，这里要么阻塞 90s 超时、要么消息延后出现。
	u := waitHubText(t, store, "精确投递")
	if u.ChatID != info.ID {
		t.Fatalf("消息应投给指定会话 %s，实际 %s", info.ID, u.ChatID)
	}
}

// 端到端验证 /model 守卫真的拦在投递之前：claude 会话收到 /model claude
// 应返回稳定 key 且不产生任何 Hub user 消息（= 没投递）。
func TestSendChatPromptRejectsUnparsableModelCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	// 必须配置平台模型：未配置时守卫按「无从判断」放行（见 checkModelSlash）。
	a.opts.Config.Model = config.ModelConfig{
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}},
		},
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = a.SendChatPrompt(info.ID, "/model claude")
	if err == nil {
		t.Fatal("/model claude 应被拒绝")
	}
	if !strings.Contains(err.Error(), "err.chat.model_not_available") {
		t.Fatalf("应返回 err.chat.model_not_available，got %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("被拒绝的指令不应落 Hub 用户消息，got %d 条", len(got))
	}
}

// 内置别名与平台模型名必须放行：/model opus 是 CLI 能解析的合法指令。
func TestSendChatPromptAllowsBuiltinAliasModelCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := a.SendChatPrompt(info.ID, "/model opus"); err != nil {
		t.Fatalf("/model opus 应放行，got %v", err)
	}
	// 等异步 turn 收尾：投递在 goroutine 里跑，测试提前返回会让 TempDir 清理
	// 与异步落盘竞争（flaky "RemoveAll cleanup: directory not empty"）。
	waitHubKind(t, store, "agent")
}


// Bugbot 审查发现的回归保护：ACP 聊天窗口的纯文本主路径是 SendChatPromptDirect，
// 守卫若只加在 sendChatPromptVia，用户从聊天框输入 /model claude 会打穿并触发 400。
func TestSendChatPromptDirectRejectsUnparsableModelCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	a.opts.Config.Model = config.ModelConfig{
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}},
		},
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = a.SendChatPromptDirect(info.ID, "/model claude")
	if err == nil {
		t.Fatal("直投入口也必须拒绝 /model claude")
	}
	if !strings.Contains(err.Error(), "err.chat.model_not_available") {
		t.Fatalf("应返回 err.chat.model_not_available，got %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("被拒绝的指令不应落 Hub 用户消息，got %d 条", len(got))
	}
}

// 直投入口的合法用法不能被误拦：内置别名与普通正文都要放行。
func TestSendChatPromptDirectAllowsNormalInput(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	a.opts.Config.Model = config.ModelConfig{
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}},
		},
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 普通正文里提到 /model 不应被当成指令
	if err := a.SendChatPromptDirect(info.ID, "请把 /model claude 改掉"); err != nil {
		t.Fatalf("普通正文应放行，got %v", err)
	}
	waitHubKind(t, store, "agent")
}

// 附件路径（SendChatPromptBlocks）同样要过守卫。
func TestSendChatPromptBlocksRejectsUnparsableModelCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	a.opts.Config.Model = config.ModelConfig{
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}},
		},
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	err = a.SendChatPromptBlocks(info.ID, []ChatBlock{{Type: "text", Text: "/model claude"}})
	if err == nil || !strings.Contains(err.Error(), "err.chat.model_not_available") {
		t.Fatalf("附件路径也应拒绝，got %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("被拒绝的指令不应落 Hub 用户消息，got %d 条", len(got))
	}
}

// Bugbot 第二轮审查发现：`#f` 会剥掉指令前缀后直接 sendAndRecord，
// 剥出的正文若是 /model 指令会绕过守卫（`#f /model claude`）。
func TestSendChatPromptFollowRejectsUnparsableModelCommand(t *testing.T) {
	a, store, rec := newDirectTestApp(t)
	a.opts.Config.Model = config.ModelConfig{
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash"}},
		},
	}
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// #f 的目标是「最近的 agent 会话」，先落一条 agent 消息把它指到这个会话。
	if err := store.Append(hub.Message{
		Kind: "agent", ChatID: info.ID, Tool: "claude", SessionTitle: "t", Text: "上一条回复",
	}); err != nil {
		t.Fatalf("append agent message: %v", err)
	}

	err = a.SendChatPrompt(info.ID, "#f /model claude")
	if err == nil {
		t.Fatal("#f 剥出的 /model claude 也必须被拒绝")
	}
	if !strings.Contains(err.Error(), "err.chat.model_not_available") {
		t.Fatalf("应返回 err.chat.model_not_available，got %v", err)
	}
	// 被拒绝时不应产生新的 user 消息（只有那条 agent 回复）
	for _, mm := range store.List() {
		if mm.Kind == "user" {
			t.Fatalf("#f 被拒绝后不应落 user 消息: %+v", mm)
		}
	}
}
