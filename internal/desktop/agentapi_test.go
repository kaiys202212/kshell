package desktop

import (
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/providers"
)

// agentApp 构造带磁盘会话的 App（复用 wxexecutor 测试基建）。
func agentApp(t *testing.T, sessions []providers.Session) *App {
	t.Helper()
	return wxApp(t, sessions)
}

// AgentBackend DiskSessions 只含未激活磁盘会话；ArchivedIDs 走归档名单。
func TestAgentBackendDiskAndArchive(t *testing.T) {
	app := agentApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "甲", UpdatedAt: time.Now()},
		{ID: "s2", ToolID: "codex", Workspace: `D:\ws\b`, Title: "乙", UpdatedAt: time.Now()},
	})
	if err := app.archives().Add("s2"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	b := &agentBackend{a: app, w: newWXExecutor(app), feed: app.agentFeedSource()}

	disk := b.DiskSessions()
	if len(disk) != 2 {
		t.Fatalf("磁盘会话应 2 条: %+v", disk)
	}
	if disk[0].Status != agentapi.StatusDisk || disk[0].ID != "" || disk[0].SessionID != "s1" {
		t.Fatalf("磁盘会话形态不符: %+v", disk[0])
	}
	ids := b.ArchivedIDs()
	if len(ids) != 1 || ids[0] != "s2" {
		t.Fatalf("归档名单不符: %v", ids)
	}

	// API 层 archived scope：s2 标记 archived。
	api := agentapi.New(b)
	arch, err := api.List(agentapi.ScopeArchived)
	if err != nil || len(arch) != 1 || arch[0].SessionID != "s2" || arch[0].Status != agentapi.StatusArchived {
		t.Fatalf("archived scope 不符: %+v err=%v", arch, err)
	}
	// Info 按 SessionID 解析磁盘会话。
	if s, ok := api.Info("s1"); !ok || s.Title != "甲" {
		t.Fatalf("Info 磁盘会话失败: %+v ok=%v", s, ok)
	}
}

// wireAgentFeed：Prompt 触发的 user update 应进入 Feed（SessionID 已翻回链头空间）。
func TestWireAgentFeedUpdate(t *testing.T) {
	app := agentApp(t, nil)
	m := newChatManager(app, fakeChatBackend{})
	t.Cleanup(m.CloseAll)
	app.mu.Lock()
	app.opts.Chats = m
	app.mu.Unlock()
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: `D:\ws\a`, Title: "甲会话", ToolID: "claude"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	ch, cancel := app.agentFeedSource().Subscribe(info.ID, 8)
	defer cancel()
	if err := m.Prompt(info.ID, "你好"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	select {
	case e := <-ch:
		if e.Kind != agentapi.EventUpdate || e.SessionID != info.ID || e.Text != "你好" {
			t.Fatalf("Feed 事件不符: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Feed 未收到 user update")
	}
}

// AgentAPI 懒构造单例；Info 与 Send 走同一套解析。
func TestAgentAPISingletonAndSend(t *testing.T) {
	app := agentApp(t, nil)
	m := newChatManager(app, fakeChatBackend{})
	t.Cleanup(m.CloseAll)
	app.mu.Lock()
	app.opts.Chats = m
	app.mu.Unlock()
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: `D:\ws\a`, Title: "甲会话", ToolID: "claude"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	api1 := app.AgentAPI()
	api2 := app.AgentAPI()
	if api1 != api2 {
		t.Fatal("AgentAPI 应返回同一实例")
	}
	s, ok := api1.Info(info.ID)
	if !ok || s.Title != "甲会话" {
		t.Fatalf("Info 活跃会话失败: %+v ok=%v", s, ok)
	}
	if err := api1.Send(info.ID, "工具指令"); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
