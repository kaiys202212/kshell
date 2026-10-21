package desktop

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
	"github.com/yangk/kshell/internal/weixin"
)

// wxSessionsResult 是注入 GetSessions（磁盘会话）用的扫描结果构造辅助。
func wxScanResult(sessions []providers.Session) *discovery.Result {
	return &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}
}

// wxApp 构造带磁盘会话/工作区数据与 hub store 的 App（无 Chats 管理器：仅磁盘会话路径）。
func wxApp(t *testing.T, sessions []providers.Session) *App {
	t.Helper()
	home := t.TempDir()
	store := hub.New(home+"/hub", 0)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	app := NewAppWith(Options{
		Home:      home,
		CachePath: home + "/cache/index.json",
		HubStore:  store,
		Windows:   NewWindowManager(&stubLauncher{}, nil),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return wxScanResult(sessions), nil
		},
		Emit: func(string, ...any) {},
	})
	app.runScan()
	return app
}

// 编译期保证 wxExecutor 满足 Executor 接口。
var _ hub.Executor = (*wxExecutor)(nil)

// ListSessions 合并两路数据并去重：同 SessionID 只留活跃版；无 Chats 时仅磁盘会话。
func TestWXExecutorListSessionsDedup(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "磁盘版本", UpdatedAt: time.Now()},
		{ID: "s2", ToolID: "codex", Workspace: `D:\ws\b`, Title: "仅磁盘", UpdatedAt: time.Now()},
	})
	// s1 已激活：活跃版标题优先。
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	if _, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, SessionID: "s1", Workspace: `D:\ws\a`, Title: "活跃版本", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	app.mu.Lock()
	app.opts.Chats = m
	app.mu.Unlock()

	ex := newWXExecutor(app)
	list := ex.ListSessions()
	if len(list) != 2 {
		t.Fatalf("应合并去重为 2 条, got %d: %+v", len(list), list)
	}
	// 活跃在前：第一条是 s1 的活跃版。
	first := list[0]
	if first.ChatID == "" || first.SessionID != "s1" || first.Title != "活跃版本" {
		t.Fatalf("首条应为 s1 活跃版: %+v", first)
	}
	// 第二条是仅磁盘的 s2：ChatID 空、SessionID 非空。
	second := list[1]
	if second.ChatID != "" || second.SessionID != "s2" || second.Title != "仅磁盘" {
		t.Fatalf("第二条应为 s2 磁盘版: %+v", second)
	}
}

// 会话引用携带工具展示名（微信欢迎词归因用）：展示名来自扫描结果；查不到为空，
// 由命令层回落 ToolID。
func TestWXExecutorToolName(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "会话A", UpdatedAt: time.Now()},
		{ID: "s2", ToolID: "unknown-tool", Workspace: `D:\ws\b`, Title: "会话B", UpdatedAt: time.Now()},
	})
	app.mu.Lock()
	app.tools = []discovery.Tool{{ID: "claude", Name: "Claude Code", Installed: true, ACP: &providers.ACPDetection{Available: true}}}
	app.mu.Unlock()

	ex := newWXExecutor(app)
	bySession := map[string]hub.SessionRef{}
	for _, r := range ex.ListSessions() {
		bySession[r.SessionID] = r
	}
	if got := bySession["s1"].ToolName; got != "Claude Code" {
		t.Fatalf("s1 应带工具展示名 Claude Code, got %q", got)
	}
	if got := bySession["s2"].ToolName; got != "" {
		t.Fatalf("未知工具展示名应为空（消费方回落 ToolID）, got %q", got)
	}
}

// wxActiveApp 构造带两个活跃新建会话的 App，返回 manager 与 executor。
func wxActiveApp(t *testing.T) (*App, *chat.Manager, *wxExecutor) {
	t.Helper()
	app := wxApp(t, nil)
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	if _, err := m.Open("new:1", chat.Info{
		Kind: chat.KindNew, Workspace: `D:\ws\a`, Title: "甲会话", ToolID: "claude",
	}, chat.Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := m.Open("new:2", chat.Info{
		Kind: chat.KindNew, Workspace: `D:\ws\b`, Title: "乙会话", ToolID: "codex",
	}, chat.Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	app.mu.Lock()
	app.opts.Chats = m
	app.mu.Unlock()
	return app, m, newWXExecutor(app)
}

// 无 hub 记录或消息都无归因时：ok=false，不再回落第一个 chat。
func TestWXExecutorLastActivityNoAttribution(t *testing.T) {
	app, _, ex := wxActiveApp(t)
	if _, _, ok := ex.LastActivity(); ok {
		t.Fatal("无 hub 记录时不应有归因")
	}
	store := app.snapshot().HubStore
	if store == nil {
		t.Fatal("wxApp 未装配 HubStore")
	}
	// 有会话、有 hub 消息，但消息不带任何归因字段：仍视为无精确目标。
	_ = store.Append(hub.Message{Kind: "agent", Text: "hi"})
	if _, _, ok := ex.LastActivity(); ok {
		t.Fatal("无归因字段的消息不得回落第一个 chat")
	}
}

// 最新消息 ChatID 精确命中活跃 chat。
func TestWXExecutorLastActivityChatID(t *testing.T) {
	app, m, ex := wxActiveApp(t)
	store := app.snapshot().HubStore
	_ = store.Append(hub.Message{Kind: "user", ChatID: app.originalChatID(listLastID(m)), Text: "hi"})
	msgs := store.List()
	gotID, ref, ok := ex.LastActivity()
	if !ok || ref.Title != "乙会话" || ref.ChatID != app.originalChatID(listLastID(m)) {
		t.Fatalf("应按 ChatID 命中乙会话: %+v ok=%v", ref, ok)
	}
	if gotID != msgs[len(msgs)-1].ID {
		t.Fatalf("应返回命中消息的 ID: got %d want %d", gotID, msgs[len(msgs)-1].ID)
	}
}

// 最新消息是旧 ChatID（已失效）时继续往前扫，TermKey 精确命中活跃 chat。
func TestWXExecutorLastActivityTermKey(t *testing.T) {
	app, m, ex := wxActiveApp(t)
	store := app.snapshot().HubStore
	list := m.List()
	first := list[0]
	_ = store.Append(hub.Message{Kind: "user", TermKey: first.Key, Text: "hi"})
	// 更旧的一条无效归因不影响往前扫。
	_ = store.Append(hub.Message{Kind: "agent", Text: "noise"})
	msgs := store.List()
	gotID, ref, ok := ex.LastActivity()
	if !ok || ref.ChatID != app.originalChatID(first.ID) || ref.Key != first.Key {
		t.Fatalf("应按 TermKey 命中甲会话: %+v ok=%v", ref, ok)
	}
	if gotID != msgs[len(msgs)-2].ID {
		t.Fatalf("应返回命中消息的 ID: got %d want %d", gotID, msgs[len(msgs)-2].ID)
	}
}

// 旧消息的 SessionID 快照命中磁盘会话：返回未激活引用（ChatID 空）。
func TestWXExecutorLastActivitySessionSnapshot(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s2", ToolID: "codex", Workspace: `D:\ws\b`, Title: "磁盘会话", UpdatedAt: time.Now()},
	})
	store := app.snapshot().HubStore
	_ = store.Append(hub.Message{Kind: "user", SessionID: "s2", Text: "hi"})
	ex := newWXExecutor(app)
	_, ref, ok := ex.LastActivity()
	if !ok || ref.SessionID != "s2" || ref.ChatID != "" || ref.Title != "磁盘会话" {
		t.Fatalf("应按 SessionID 命中未激活磁盘会话: %+v ok=%v", ref, ok)
	}

	// termKey session: 前缀同样视为磁盘会话快照。
	app2 := wxApp(t, []providers.Session{
		{ID: "s3", ToolID: "claude", Workspace: `D:\ws\c`, Title: "前缀会话", UpdatedAt: time.Now()},
	})
	_ = app2.snapshot().HubStore.Append(hub.Message{Kind: "agent", TermKey: "session:s3", Text: "hi"})
	_, ref, ok = newWXExecutor(app2).LastActivity()
	if !ok || ref.SessionID != "s3" || ref.ChatID != "" {
		t.Fatalf("termKey session: 前缀应命中磁盘会话: %+v ok=%v", ref, ok)
	}
}

// 会话文件已消失但仍带 SessionID 快照：按快照返回未激活引用（激活失败可见），
// 不回落到更旧消息的归因。
func TestWXExecutorLastActivityGoneSessionSnapshot(t *testing.T) {
	app := wxApp(t, nil)
	store := app.snapshot().HubStore
	_ = store.Append(hub.Message{
		Kind: "user", SessionID: "gone", Workspace: `D:\ws\z`, Tool: "claude",
		SessionTitle: "旧会话", Text: "hi",
	})
	_, ref, ok := newWXExecutor(app).LastActivity()
	if !ok || ref.SessionID != "gone" || ref.ChatID != "" || ref.Title != "旧会话" || ref.Tool != "claude" {
		t.Fatalf("应按 SessionID 快照返回未激活引用: %+v ok=%v", ref, ok)
	}
	if ref.ToolName != "" {
		t.Fatalf("未知工具展示名应为空, got %q", ref.ToolName)
	}
}

// 最新一条带归因却解析不到（失效 ChatID）时停止扫描：不得借用更旧归因误投。
func TestWXExecutorLastActivityStaleLatestStopsScan(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "旧会话", UpdatedAt: time.Now()},
	})
	store := app.snapshot().HubStore
	_ = store.Append(hub.Message{Kind: "user", TermKey: "session:s1", Text: "old"})
	_ = store.Append(hub.Message{Kind: "agent", ChatID: "c-gone", Text: "new"})
	if _, ref, ok := newWXExecutor(app).LastActivity(); ok {
		t.Fatalf("最新归因失效时不得回落到更旧会话: %+v", ref)
	}
}

// AgentHub 直接投递（不经 Console）后，桌面端 Console 的待选菜单必须失效：
// 之后的纯数字不得发送旧暂存正文。（HubPickTarget 不写历史；直接投递路径
// 落盘「另一条」，数字再发送「待发正文」即失败。）
func TestHubSendClearsPendingPick(t *testing.T) {
	app, m, _ := wxActiveApp(t)
	if _, err := app.HubPickTarget("待发正文"); err != nil {
		t.Fatalf("pick: %v", err)
	}
	if err := app.SendChatPrompt(m.List()[0].ID, "另一条"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := app.HubCommand("1"); err != nil {
		t.Fatalf("number: %v", err)
	}
	for _, msg := range app.snapshot().HubStore.List() {
		if msg.Kind == "user" && msg.Text == "待发正文" {
			t.Fatalf("直接投递后旧暂存正文不得被数字发送: %+v", msg)
		}
	}
}

// 桌面直发成功后须同时清微信桥 Console 的待选，避免微信侧数字误发旧暂存。
func TestHubSendClearsWeixinBridgePendingPick(t *testing.T) {
	app, m, _ := wxActiveApp(t)
	r := weixin.NewRuntime(newWXExecutor(app), "zh-CN")
	r.SetPersist(func(weixin.WeixinPersist) error { return nil })
	r.Configure("tok", "u1")
	app.mu.Lock()
	app.wx = r
	app.mu.Unlock()
	bridge := r.GetBridge()
	if bridge == nil {
		t.Fatal("应有 bridge")
	}
	replies, pending := bridge.PickTarget("微信暂存")
	if !pending || len(replies) == 0 {
		t.Fatalf("微信侧应建立待选: pending=%v replies=%v", pending, replies)
	}
	if err := app.SendChatPrompt(m.List()[0].ID, "桌面直发"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// 微信桥再回数字不得投递「微信暂存」。
	out := bridge.HandleConsole("2")
	for _, r := range out {
		if r.Sent {
			t.Fatalf("桌面直发后微信待选应已失效: %+v", out)
		}
	}
	for _, msg := range app.snapshot().HubStore.List() {
		if msg.Kind == "user" && msg.Text == "微信暂存" {
			t.Fatalf("微信旧暂存不得被数字发送: %+v", msg)
		}
	}
}

// 待选建立 → 回数字：Console 经 wxExecutor.Send 走真实 SendChatPrompt。这是
// 唯一让「Handle 持 c.mu」与「发送成功 → clearHubPendingPick」相遇的链路
// （回归：clearHubPendingPick 若阻塞等 c.mu 即不可重入死锁，须走 TryLock）。
// 回复带 Sent 标记，且菜单已清——再回数字不再重复发送。暂存正文由发送链
// sendAndRecord 落盘（可见性由前端本地渲染，HubPickTarget 不写历史）。
func TestHubPickNumberSendDeliversPending(t *testing.T) {
	app, _, _ := wxActiveApp(t)
	if _, err := app.HubPickTarget("待发正文"); err != nil {
		t.Fatalf("pick: %v", err)
	}
	replies, err := app.HubCommand("2")
	if err != nil {
		t.Fatalf("number: %v", err)
	}
	sent := false
	for _, r := range replies {
		// 测试 App 默认英文文案（sent_to = "Sent to: ..."），Sent 标记是稳定契约。
		if r.Sent && strings.HasPrefix(r.Text, "Sent to") {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("回数字应发送暂存正文并标记 Sent: %+v", replies)
	}
	delivered := 0
	for _, msg := range app.snapshot().HubStore.List() {
		if msg.Kind == "user" && msg.Text == "待发正文" {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("暂存正文应恰好落盘一次: %d", delivered)
	}
	// 菜单已清：再回数字只按最近列表切换，不得重复发送旧正文。
	if _, err := app.HubCommand("2"); err != nil {
		t.Fatalf("number again: %v", err)
	}
	delivered = 0
	for _, msg := range app.snapshot().HubStore.List() {
		if msg.Kind == "user" && msg.Text == "待发正文" {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("菜单已清后数字不得重复发送旧正文: %d", delivered)
	}
}

// RecoverSession：hub 里旧 chatID 带 SessionID 快照时，找回同 SessionID 的活跃/磁盘引用。
func TestWXExecutorRecoverSession(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s-recover", ToolID: "claude", Workspace: `D:\ws\a`, Title: "可恢复", UpdatedAt: time.Now()},
	})
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	if _, err := m.Open("new:1", chat.Info{
		Kind: chat.KindSession, SessionID: "s-recover", Workspace: `D:\ws\a`,
		Title: "可恢复", ToolID: "claude",
	}, chat.Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	app.mu.Lock()
	app.opts.Chats = m
	app.mu.Unlock()

	oldID := "c-closed-old"
	if err := app.snapshot().HubStore.Append(hub.Message{
		Kind: "user", ChatID: oldID, SessionID: "s-recover",
		Tool: "claude", SessionTitle: "可恢复", Text: "旧",
	}); err != nil {
		t.Fatal(err)
	}
	ex := newWXExecutor(app)
	ref, ok := ex.RecoverSession(oldID)
	if !ok {
		t.Fatal("应恢复到同 SessionID 会话")
	}
	if ref.SessionID != "s-recover" {
		t.Fatalf("SessionID = %q", ref.SessionID)
	}
	if ref.ChatID == "" {
		t.Fatal("活跃会话应带回 ChatID")
	}
	if _, ok := ex.RecoverSession("never-seen"); ok {
		t.Fatal("未知 chatID 不得恢复")
	}
}

// listLastID 取 manager 列表最后一条的 ID（ListChats 顺序同 manager.order）。
func listLastID(m *chat.Manager) string {
	l := m.List()
	if len(l) == 0 {
		return ""
	}
	return l[len(l)-1].ID
}

// SearchSessions 对 Title/Workspace/Tool 不区分大小写包含匹配。
func TestWXExecutorSearchSessions(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\Login`, Title: "修复上传白名单", UpdatedAt: time.Now()},
		{ID: "s2", ToolID: "codex", Workspace: `D:\ws\b`, Title: "无关会话", UpdatedAt: time.Now()},
	})
	ex := newWXExecutor(app)
	if got := ex.SearchSessions("白名单"); len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("按标题搜索应命中 s1: %+v", got)
	}
	if got := ex.SearchSessions("LOGIN"); len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("工作区搜索应不区分大小写命中 s1: %+v", got)
	}
	if got := ex.SearchSessions("CODEX"); len(got) != 1 || got[0].SessionID != "s2" {
		t.Fatalf("按工具搜索应命中 s2: %+v", got)
	}
	if got := ex.SearchSessions("不存在"); len(got) != 0 {
		t.Fatalf("无匹配应返回空: %+v", got)
	}
}

// EasyNew 无可用工具时报错（wire key 风格）。
func TestWXExecutorEasyNewNoTools(t *testing.T) {
	app := wxApp(t, nil)
	ex := newWXExecutor(app)
	_, err := ex.EasyNew()
	if err == nil || !strings.Contains(err.Error(), "err.") {
		t.Fatalf("无工具应返回 wire key 错误, got %v", err)
	}
}

// ListSessions 为空时各方法不得 panic：current 缺失、搜索空、状态零值。
func TestWXExecutorEmptySessionsNoPanic(t *testing.T) {
	app := wxApp(t, nil)
	ex := newWXExecutor(app)
	if _, _, ok := ex.LastActivity(); ok {
		t.Fatal("空会话时 LastActivity 应 ok=false")
	}
	if got := ex.SearchSessions("x"); len(got) != 0 {
		t.Fatalf("空会话搜索应为空: %+v", got)
	}
	if err := ex.SwitchTo("nope"); err == nil {
		t.Fatal("不存在的 chatID 应报错")
	}
	st := ex.Status()
	if st.Running != 0 || st.Undelivered != 0 {
		t.Fatalf("空会话状态应为零值: %+v", st)
	}
}

// GrantPermission 的允许/拒绝转发到 RespondChatPermission 的真实 optionID。
func TestWXExecutorGrantPermissionOptionIDs(t *testing.T) {
	app := wxApp(t, nil)
	ex := newWXExecutor(app)
	if err := ex.GrantPermission("c1", "r1", true); err != nil && !errors.Is(err, errNotReady) {
		t.Fatalf("allow 转发不应失败（Chats 为 nil 时 errNotReady 可接受）: %v", err)
	}
	if err := ex.GrantPermission("c1", "r1", false); err != nil && !errors.Is(err, errNotReady) {
		t.Fatalf("reject 转发不应失败（Chats 为 nil 时 errNotReady 可接受）: %v", err)
	}
}

// easyNewToolID：配置的 AgentHub 默认工具优先，失效（未安装）回落第一个已安装。
func TestWXExecutorEasyNewDefaultTool(t *testing.T) {
	app := wxApp(t, nil)
	ex := newWXExecutor(app)
	app.mu.Lock()
	app.tools = []discovery.Tool{
		{ID: "claude", Installed: true},
		{ID: "codex", Installed: true},
	}
	app.mu.Unlock()

	// 未配置：回落第一个已安装（字母序 claude 在前）。
	if got := ex.easyNewToolID(); got != "claude" {
		t.Fatalf("未配置默认工具应回落 claude, got %q", got)
	}
	// 配置 codex：优先使用配置。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "codex"
	app.mu.Unlock()
	if got := ex.easyNewToolID(); got != "codex" {
		t.Fatalf("配置的默认工具应生效, got %q", got)
	}
	// 配置的工具未安装：回落第一个已安装。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "gone"
	app.mu.Unlock()
	if got := ex.easyNewToolID(); got != "claude" {
		t.Fatalf("失效默认工具应回落 claude, got %q", got)
	}
	// 工具表里根本没有该项（连 Installed 字段都没有）：同样回落。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "unknown-tool"
	app.mu.Unlock()
	if got := ex.easyNewToolID(); got != "claude" {
		t.Fatalf("未知默认工具应回落 claude, got %q", got)
	}
}

// 规则1/2：非 ACP 工具的磁盘会话不进命令层列表；终端激活态的 ACP 会话保留
// 并标 TerminalActive；工具能力未知时保守保留（不过滤）。
func TestWXExecutorListSessionsReplyability(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s-acp", ToolID: "claude", Workspace: `D:\ws\a`, Title: "ACP 会话", UpdatedAt: time.Now()},
		{ID: "s-noacp", ToolID: "codex", Workspace: `D:\ws\b`, Title: "非 ACP 会话", UpdatedAt: time.Now()},
		{ID: "s-unknown", ToolID: "mystery", Workspace: `D:\ws\c`, Title: "未知工具", UpdatedAt: time.Now()},
	})
	app.mu.Lock()
	app.tools = []discovery.Tool{
		{ID: "claude", Name: "Claude Code", Installed: true, ACP: &providers.ACPDetection{Available: true}},
		{ID: "codex", Name: "Codex", Installed: true}, // 无 ACP 探测 → 不支持
	}
	app.mu.Unlock()

	ex := newWXExecutor(app)
	byID := map[string]hub.SessionRef{}
	for _, r := range ex.ListSessions() {
		byID[r.SessionID] = r
	}
	if _, ok := byID["s-noacp"]; ok {
		t.Fatalf("非 ACP 工具的会话不应进列表: %+v", byID)
	}
	if _, ok := byID["s-unknown"]; !ok {
		t.Fatalf("能力未知的工具会话应保守保留: %+v", byID)
	}
	if r := byID["s-acp"]; r.TerminalActive {
		t.Fatalf("未在终端打开的会话不应标 TerminalActive: %+v", r)
	}
}

// 终端在册的磁盘会话标 TerminalActive；OpenSession 对其报 err.hub.terminal_active。
func TestWXExecutorTerminalActiveSession(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "会话A", UpdatedAt: time.Now()},
	})
	app.mu.Lock()
	app.tools = []discovery.Tool{{ID: "claude", Installed: true, ACP: &providers.ACPDetection{Available: true}}}
	app.mu.Unlock()
	tm := newTerminalManagerWith(func(string, ...any) {}, &stubTermBackend{})
	app.mu.Lock()
	app.opts.Terminals = tm
	app.mu.Unlock()
	if _, err := tm.Open("session:s1", terminal.Info{
		Kind: terminal.KindSession, SessionID: "s1", Workspace: "D:/ws", Title: "会话A", ToolID: "claude",
	}, terminal.Spec{Path: "x"}, 80, 24); err != nil {
		t.Fatalf("open terminal: %v", err)
	}

	ex := newWXExecutor(app)
	list := ex.ListSessions()
	if len(list) != 1 || !list[0].TerminalActive {
		t.Fatalf("终端在册的磁盘会话应标 TerminalActive: %+v", list)
	}
	if _, err := ex.OpenSession("s1"); err == nil || !strings.Contains(err.Error(), "err.hub.terminal_active") {
		t.Fatalf("终端激活态应报 err.hub.terminal_active, got %v", err)
	}
}

// LastActivity：最新可归因为已知非 ACP 时停止扫描，返回不可投递引用
//（空 ChatID），不借用更旧的 ACP 会话。
func TestWXExecutorLastActivityNonACPStopsScan(t *testing.T) {
	app, m, ex := wxActiveApp(t)
	app.mu.Lock()
	app.tools = []discovery.Tool{
		{ID: "codex", Installed: true}, // 无 ACP
		{ID: "claude", Name: "Claude Code", Installed: true, ACP: &providers.ACPDetection{Available: true}},
	}
	app.mu.Unlock()
	store := app.snapshot().HubStore
	oldID := app.originalChatID(listLastID(m))
	_ = store.Append(hub.Message{Kind: "user", ChatID: oldID, Tool: "claude", Text: "old"})
	if err := store.Append(hub.Message{Kind: "agent", Tool: "codex", TermKey: "session:s9", Text: "终端回复"}); err != nil {
		t.Fatal(err)
	}

	_, ref, ok := ex.LastActivity()
	if !ok {
		t.Fatal("非 ACP 最新消息应返回水位（供清除 current），ok 须为 true")
	}
	if ref.ChatID != "" {
		t.Fatalf("非 ACP 不得作为可投递目标（也不得命中更旧 ACP %s）: %+v", oldID, ref)
	}
	if ref.Tool != "codex" {
		t.Fatalf("应带回非 ACP 工具标记: %+v", ref)
	}
}
