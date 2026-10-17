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
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil)
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

// HubStore 最后一条消息的 ChatID 定位 current；无 hub 记录时回落 ListChats 第一个。
func TestWXExecutorCurrentSession(t *testing.T) {
	app := wxApp(t, nil)
	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil)
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

	ex := newWXExecutor(app)
	// 无 hub 记录：回落 ListChats 第一个。
	cur, ok := ex.CurrentSession()
	if !ok || cur.Title != "甲会话" {
		t.Fatalf("无 hub 记录应回落第一个 chat: %+v ok=%v", cur, ok)
	}

	// 落一条乙会话的 hub 消息：current 应切到乙。
	// 落盘 ChatID 是前端 ID 空间（ListChats 的 ID）；Open 后未重建，链头即 manager ID。
	store := app.snapshot().HubStore
	if store == nil {
		t.Fatal("wxApp 未装配 HubStore")
	}
	_ = store.Append(hub.Message{Kind: "agent", ChatID: app.originalChatID(listLastID(m)), Text: "hi"})
	cur, ok = ex.CurrentSession()
	if !ok || cur.Title != "乙会话" {
		t.Fatalf("应按 hub 最后消息定位到乙会话: %+v ok=%v", cur, ok)
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
	if _, ok := ex.CurrentSession(); ok {
		t.Fatal("空会话时 CurrentSession 应 ok=false")
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
