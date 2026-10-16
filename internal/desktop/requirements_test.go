package desktop

// 需求归档 / 状态流转的绑定层行为测试：
// 归档 done 需求时释放其关联的空闲聊天会话，忙碌会话保持不动。

import (
	"testing"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/requirements"
)

// newRequirementTestApp 装配带需求 store 与聊天管理器的 App。
// backend 为 nil 时用默认桩后端；返回 rec 供断言事件推送。
func newRequirementTestApp(t *testing.T, backend chat.Backend) (*App, *requirements.Store, *chat.Manager, *notifyRecorder) {
	t.Helper()
	store := requirements.New(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	rec := &notifyRecorder{}
	if backend == nil {
		backend = &fakeChatCaptureBackend{}
	}
	m := newChatManagerWith(rec.emit, backend, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	app := NewAppWith(Options{Chats: m, RequirementsStore: store, Emit: rec.emit})
	return app, store, m, rec
}

// openTestChat 用桩后端开一个空闲（ready）聊天会话。
func openTestChat(t *testing.T, m *chat.Manager) chat.Info {
	t.Helper()
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open chat: %v", err)
	}
	return info
}

// TestArchiveRequirementReleasesIdleChat 归档 done 需求时，
// 空闲（ready）的关联会话应被关闭释放并返回 Released=true。
func TestArchiveRequirementReleasesIdleChat(t *testing.T) {
	app, store, m, rec := newRequirementTestApp(t, nil)
	it, err := store.Add("归档释放", "high", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := openTestChat(t, m)
	if err := store.MarkDispatched(it.ID, "/w", info.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(it.ID, requirements.StatusDone); err != nil {
		t.Fatal(err)
	}

	res, err := app.ArchiveRequirement(it.ID)
	if err != nil {
		t.Fatalf("ArchiveRequirement: %v", err)
	}
	if !res.Released || res.ChatID != info.ID {
		t.Fatalf("应释放空闲会话, got %+v", res)
	}
	if _, ok := m.Get(info.ID); ok {
		t.Fatal("空闲会话应被关闭并从 manager 摘除")
	}
	for _, cur := range store.List() {
		if cur.ID == it.ID && cur.Status != requirements.StatusArchived {
			t.Fatalf("需求应已归档, got %q", cur.Status)
		}
	}
	if rec.count("requirements:changed") == 0 {
		t.Fatal("归档后应推送 requirements:changed")
	}
}

// TestArchiveRequirementKeepsBusyChat 执行中（running）的会话不释放：
// Released=false 且不报错，需求本身仍归档成功，会话保持运行。
func TestArchiveRequirementKeepsBusyChat(t *testing.T) {
	conn := &fakeChatConn{waitCh: make(chan struct{}), promptGate: make(chan struct{})}
	app, store, m, _ := newRequirementTestApp(t, &fakeChatCaptureBackend{conn: conn})
	it, err := store.Add("忙碌归档", "high", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := openTestChat(t, m)
	if err := store.MarkDispatched(it.ID, "/w", info.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(it.ID, requirements.StatusDone); err != nil {
		t.Fatal(err)
	}
	// 造 running：promptGate 阻塞 Prompt 模拟执行中的轮次
	if err := m.Prompt(info.ID, "干活中"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got, ok := m.Get(info.ID); !ok || got.Status != chat.StatusRunning {
		t.Fatalf("前置：会话应为 running, got %+v ok=%v", got, ok)
	}

	res, err := app.ArchiveRequirement(it.ID)
	if err != nil {
		t.Fatalf("忙碌会话归档不应报错: %v", err)
	}
	if res.Released {
		t.Fatalf("执行中的会话不应释放, got %+v", res)
	}
	if got, ok := m.Get(info.ID); !ok || got.Status != chat.StatusRunning {
		t.Fatalf("执行中会话不应被关闭, got %+v ok=%v", got, ok)
	}
	for _, cur := range store.List() {
		if cur.ID == it.ID && cur.Status != requirements.StatusArchived {
			t.Fatalf("需求仍应归档成功, got %q", cur.Status)
		}
	}
	close(conn.promptGate) // 放行阻塞的 Prompt，让测试 goroutine 收尾
}

// TestArchiveRequirementNotDone 未完成的需求不可归档。
func TestArchiveRequirementNotDone(t *testing.T) {
	app, store, _, _ := newRequirementTestApp(t, nil)
	it, err := store.Add("未完成", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ArchiveRequirement(it.ID); err == nil {
		t.Fatal("非 done 需求归档应报错")
	}
}

// TestSetRequirementStatusBinding 绑定层透传状态机：
// active→done 允许；pending→done 非法流转报错；成功后推送 requirements:changed。
func TestSetRequirementStatusBinding(t *testing.T) {
	app, store, _, rec := newRequirementTestApp(t, nil)
	it, err := store.Add("状态流转", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.SetRequirementStatus(it.ID, requirements.StatusDone); err == nil {
		t.Fatal("pending→done 应被拒绝")
	}
	if err := app.SetRequirementStatus(it.ID, requirements.StatusActive); err != nil {
		t.Fatalf("pending→active: %v", err)
	}
	if err := app.SetRequirementStatus(it.ID, requirements.StatusDone); err != nil {
		t.Fatalf("active→done: %v", err)
	}
	for _, cur := range store.List() {
		if cur.ID == it.ID && cur.Status != requirements.StatusDone {
			t.Fatalf("需求应为 done, got %q", cur.Status)
		}
	}
	if rec.count("requirements:changed") == 0 {
		t.Fatal("状态变更后应推送 requirements:changed")
	}
}

// TestMarkRequirementDispatchedTitle 绑定层透传会话标题快照：派发时落盘 sessionTitle。
func TestMarkRequirementDispatchedTitle(t *testing.T) {
	app, store, m, rec := newRequirementTestApp(t, nil)
	it, err := store.Add("带标题派发", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := openTestChat(t, m)

	if err := app.MarkRequirementDispatched(it.ID, "/w", info.ID, "会话标题"); err != nil {
		t.Fatalf("MarkRequirementDispatched: %v", err)
	}
	for _, cur := range store.List() {
		if cur.ID == it.ID && cur.SessionTitle != "会话标题" {
			t.Fatalf("sessionTitle = %q, want 会话标题", cur.SessionTitle)
		}
	}
	if rec.count("requirements:changed") == 0 {
		t.Fatal("派发后应推送 requirements:changed")
	}
}

// TestChatTitleChangeUpdatesRequirement 聊天标题变化（首条消息改标题）时，
// 已派发需求的 sessionTitle 快照随之更新；无关联需求的会话静默忽略。
func TestChatTitleChangeUpdatesRequirement(t *testing.T) {
	conn := &fakeChatConn{waitCh: make(chan struct{}), promptGate: make(chan struct{})}
	app, store, m, _ := newRequirementTestApp(t, &fakeChatCaptureBackend{conn: conn})
	// 钩子在 ensureSessionHooks 里接 SetOnInfo；桩 manager 需要显式接上同款回调
	m.SetOnInfo(func(info chat.Info) {
		store.SetSessionTitle(info.ID, info.Title)
	})
	it, err := store.Add("标题回填", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := openTestChat(t, m)
	if err := app.MarkRequirementDispatched(it.ID, "/w", info.ID, ""); err != nil {
		t.Fatal(err)
	}

	if err := m.Prompt(info.ID, "第一条消息"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	close(conn.promptGate) // 放行阻塞的 Prompt 让轮次收尾

	// SetOnInfo 在锁外异步触发，轮询等快照更新
	deadline := time.Now().Add(2 * time.Second)
	for {
		title := ""
		for _, cur := range store.List() {
			if cur.ID == it.ID {
				title = cur.SessionTitle
			}
		}
		if title == "第一条消息" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessionTitle 未回填, got %q", title)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 无关联需求的会话：标题变化不报错（静默忽略）
	other := openTestChat(t, m)
	if err := m.Prompt(other.ID, "未关联会话"); err != nil {
		t.Fatalf("prompt other: %v", err)
	}
}

// TestListRequirementsFillsLiveTitle ListRequirements 返回时按 ChatID 从
// chat manager 反查实时标题补入返回副本（只影响本次返回，不落盘——落盘由
// SetOnInfo hook 负责）：
//  1. manager 有该会话且快照为空 → 返回实时标题；
//  2. manager 无该会话（已关闭/重启）→ 保持落盘快照。
func TestListRequirementsFillsLiveTitle(t *testing.T) {
	app, store, m, _ := newRequirementTestApp(t, nil)
	itLive, err := store.Add("实时标题反查", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}
	itDead, err := store.Add("关闭会话保持快照", "medium", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := openTestChat(t, m)
	// openTestChat 未传 Title（空）；经 AttachSession 绑定磁盘会话并注入标题
	// （messages>0 才写标题，与生产扫描回填同路径），live 条目不落快照（模拟
	// 旧数据），dead 条目落快照且 chatId 指向不存在的会话
	if !m.AttachSession("disk-1", "/w", "", "实时标题", 3, time.Time{}) {
		t.Fatal("前置：AttachSession 应命中桩会话")
	}
	_ = info
	if err := store.MarkDispatched(itLive.ID, "/w", "c1", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDispatched(itDead.ID, "/w", "c-dead", "旧快照"); err != nil {
		t.Fatal(err)
	}

	gotList := app.ListRequirements()
	for _, cur := range gotList {
		switch cur.ID {
		case itLive.ID:
			if cur.SessionTitle != "实时标题" {
				t.Fatalf("live 条目应反查实时标题, got %q", cur.SessionTitle)
			}
		case itDead.ID:
			if cur.SessionTitle != "旧快照" {
				t.Fatalf("dead 条目应保持快照, got %q", cur.SessionTitle)
			}
		}
	}
	// 不落盘：再从 store 直读，live 条目快照仍为空
	for _, cur := range store.List() {
		if cur.ID == itLive.ID && cur.SessionTitle != "" {
			t.Fatalf("反查不应落盘, store 快照 = %q", cur.SessionTitle)
		}
	}
}
