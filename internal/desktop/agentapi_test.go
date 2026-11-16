package desktop

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
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

// AgentBackend Tools 候选只含 ACP 可用的已安装工具（管理者 create_session 的
// 候选与校验同源，非 ACP 工具不得进入，否则创建必报错）。
func TestAgentBackendToolsACPOnly(t *testing.T) {
	app := agentApp(t, nil)
	b := &agentBackend{a: app, w: newWXExecutor(app), feed: app.agentFeedSource()}
	app.mu.Lock()
	app.tools = []discovery.Tool{
		{ID: "claude", Name: "Claude Code", Installed: true, ACP: &providers.ACPDetection{Available: true}},
		{ID: "codex", Name: "Codex", Installed: true}, // 无 ACP
		{ID: "gone", Name: "Gone", Installed: false, ACP: &providers.ACPDetection{Available: true}},
	}
	app.mu.Unlock()
	opts := b.Tools()
	if len(opts) != 1 || opts[0].ID != "claude" {
		t.Fatalf("Tools 应只含 ACP 可用的已安装工具 [claude], got %+v", opts)
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

// startAgentSrv：endpoint 文件写入且服务可访问；Shutdown 清理。
func TestStartAgentSrv(t *testing.T) {
	app := agentApp(t, nil)
	path := app.agentAPIEndpointPath()
	app.startAgentSrv()
	t.Cleanup(func() {
		app.mu.Lock()
		srv := app.agentSrv
		app.agentSrv = nil
		app.mu.Unlock()
		if srv != nil {
			srv.Close()
		}
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("endpoint 文件未写入: %v", err)
	}
	var ep struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &ep); err != nil || ep.URL == "" || ep.Token == "" {
		t.Fatalf("endpoint 内容不符: %s", raw)
	}
	// 经 endpoint 访问服务。
	req, _ := http.NewRequest(http.MethodGet, ep.URL+"/api/v1/sessions?scope=active", nil)
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("endpoint 访问失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list 应 200: %d", resp.StatusCode)
	}
	// Shutdown 清理路径：agentSrv.Close 删文件。
	app.mu.Lock()
	srv := app.agentSrv
	app.mu.Unlock()
	srv.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Close 后 endpoint 文件应删除")
	}
}

// RecentHubMessages 映射归因字段并沿用 store 的滤噪口径。
func TestAgentBackendRecentHubMessages(t *testing.T) {
	app := agentApp(t, nil)
	store := app.snapshot().HubStore
	if store == nil {
		t.Fatal("HubStore 未装配")
	}
	for _, m := range []hub.Message{
		{Kind: "user", ChatID: "c1", Tool: "claude", SessionTitle: "修复登录", Text: "继续修"},
		{Kind: "agent", Event: "hub_manager_reply", Text: "已投递"},
		{Kind: "agent", Event: "ui.hub.ack_delivered", Text: "ui.hub.ack_delivered"}, // 噪声
	} {
		if err := store.Append(m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	b := &agentBackend{a: app, w: newWXExecutor(app), feed: app.agentFeedSource()}
	got := b.RecentHubMessages(10)
	if len(got) != 2 {
		t.Fatalf("应滤掉 ui.hub.* 噪声，剩 2 条: %+v", got)
	}
	if got[0].SessionID != "c1" || got[0].SessionTitle != "修复登录" || got[0].Tool != "claude" || got[0].Kind != "user" {
		t.Fatalf("归因字段映射不符: %+v", got[0])
	}
	if got[1].Event != "hub_manager_reply" || got[1].Kind != "agent" {
		t.Fatalf("agent 消息映射不符: %+v", got[1])
	}
	if got := b.RecentHubMessages(0); got != nil {
		t.Fatalf("n<=0 应返回空: %+v", got)
	}
}
