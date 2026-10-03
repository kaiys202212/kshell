package desktop

// 扫描回填（runScan → attachDiscoveredSessions）的行为测试：
// 新建会话（终端/聊天）启动时磁盘上还没有会话记录，SessionID 为空、页签是占位标题；
// 重扫发现新会话后应自动回填，页签标题与「恢复/切换」判断才能生效。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// attachTestApp 装配一个「扫描结果可变」的 App：terminal+chat 管理器都走桩后端。
func attachTestApp(t *testing.T, acpDet *providers.ACPDetection) (*App, *eventLog, func([]providers.Session)) {
	t.Helper()

	var mu sync.Mutex
	var sessions []providers.Session
	setSessions := func(list []providers.Session) {
		mu.Lock()
		defer mu.Unlock()
		sessions = list
	}
	scan := func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		return &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}, nil
	}

	events := newEventLog()
	app := NewAppWith(Options{
		Home:      "D:/home",
		CachePath: "D:/home/.kshell/cache/index.json",
		Providers: []providers.Provider{providers.Claude{}},
		Terminals: newTerminalManagerWith(events.emit, &stubTermBackend{}),
		Chats:     newChatManagerWith(events.emit, fakeChatBackend{}),
		Windows:   NewWindowManager(&stubLauncher{}, nil),
		Emit:      events.emit,
		Scan:      scan,
	})
	t.Cleanup(func() { app.Shutdown(context.Background()) })

	setTools(t, app, []discovery.Tool{{
		ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true, ACP: acpDet,
	}})
	return app, events, setSessions
}

// lastScanDone 取最后一条 scan:done 的负载。
func lastScanDone(t *testing.T, events *eventLog) map[string]any {
	t.Helper()
	for i := len(events.events) - 1; i >= 0; i-- {
		if events.events[i].name == "scan:done" {
			return events.events[i].payload
		}
	}
	t.Fatal("没有 scan:done 事件")
	return nil
}

func TestRunScanAttachesTerminal(t *testing.T) {
	app, events, setSessions := attachTestApp(t, nil)
	setSessions([]providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "旧会话", UpdatedAt: time.Now().Add(-time.Hour)},
	})
	app.runScan()

	// 新建终端：KindNew、SessionID 空、占位标题
	term, err := app.OpenWorkspaceTerminal(`D:\ws-a`, "claude", 80, 24)
	if err != nil {
		t.Fatalf("OpenWorkspaceTerminal error: %v", err)
	}
	if term.SessionID != "" || term.Kind != "new" {
		t.Fatalf("前置：新建终端应未绑定, got %+v", term)
	}

	// 重扫发现新会话 s2：应回填到运行中的新建终端
	setSessions([]providers.Session{
		providers.Session{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "旧会话", UpdatedAt: time.Now().Add(-time.Hour)},
		providers.Session{ID: "s2", ToolID: "claude", Workspace: `D:\ws-a`, Title: "新任务", UpdatedAt: time.Now()},
	})
	app.runScan()

	list := app.ListTerminals()
	var got *struct{ ID, SessionID, Title string }
	for i := range list {
		if list[i].ID == term.ID {
			got = &struct{ ID, SessionID, Title string }{list[i].ID, list[i].SessionID, list[i].Title}
		}
	}
	if got == nil {
		t.Fatal("新建终端应仍在列表中")
	}
	if got.SessionID != "s2" || got.Title != "新任务" {
		t.Fatalf("重扫后终端未绑定 s2: %+v", got)
	}
	if lastScanDone(t, events)["attached"] != true {
		t.Fatalf("发生绑定时 scan:done 应带 attached=true, got %+v", lastScanDone(t, events))
	}
}

func TestRunScanAttachesChat(t *testing.T) {
	app, events, setSessions := attachTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	})
	setSessions([]providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "旧会话", UpdatedAt: time.Now().Add(-time.Hour)},
	})
	app.runScan()

	// 新建聊天（ACP 路径）：KindNew、SessionID 空
	got, err := app.OpenWorkspace(`D:\ws-a`, "claude")
	if err != nil {
		t.Fatalf("OpenWorkspace error: %v", err)
	}
	if got.Kind != "chat" || got.Chat == nil {
		t.Fatalf("应走聊天路径: %+v", got)
	}
	if got.Chat.SessionID != "" {
		t.Fatalf("前置：新建聊天 Info.SessionID 应为空, got %q", got.Chat.SessionID)
	}

	// 重扫发现新会话 s2：应回填到聊天 Info（协议层 sessionID 不受影响）
	setSessions([]providers.Session{
		providers.Session{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "旧会话", UpdatedAt: time.Now().Add(-time.Hour)},
		providers.Session{ID: "s2", ToolID: "claude", Workspace: `D:\ws-a`, Title: "新任务", UpdatedAt: time.Now()},
	})
	app.runScan()

	var gotInfo *struct{ SessionID, Title string }
	for _, c := range app.ListChats() {
		if c.ID == got.Chat.ID {
			gotInfo = &struct{ SessionID, Title string }{c.SessionID, c.Title}
		}
		// 上轮扫描就存在的旧会话不得被绑（新建动作发生在上轮扫描之后，不可能对应它）
		if c.SessionID == "s1" {
			t.Fatalf("旧会话 s1 不应被绑定: %+v", c)
		}
	}
	if gotInfo == nil {
		t.Fatal("新建聊天应仍在列表中")
	}
	if gotInfo.SessionID != "s2" || gotInfo.Title != "新任务" {
		t.Fatalf("重扫后聊天未绑定 s2: %+v", gotInfo)
	}
	if lastScanDone(t, events)["attached"] != true {
		t.Fatalf("发生绑定时 scan:done 应带 attached=true")
	}
}

// 无可绑定目标时 scan:done 不带 attached（避免前端做无谓刷新）。
func TestRunScanNoAttachNoFlag(t *testing.T) {
	app, events, setSessions := attachTestApp(t, nil)
	setSessions([]providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "旧会话", UpdatedAt: time.Now()},
	})
	app.runScan()
	if _, ok := lastScanDone(t, events)["attached"]; ok {
		t.Fatalf("没有绑定不应带 attached, got %+v", lastScanDone(t, events))
	}
}
