// Package apitest 提供导出的 agentapi.Backend 测试桩：agentsrv / agentcli 等
// 消费方的测试共用一份（包名带 _test 语义但保持普通包，仅供 *_test.go 导入）。
package apitest

import (
	"errors"
	"fmt"

	"github.com/yangk/kshell/internal/agentapi"
)

// FakeBackend 是记录调用参数、返回预制数据的 Backend 桩。
type FakeBackend struct {
	Active      []agentapi.Session
	Disk        []agentapi.Session
	Archived    []string
	Msgs        map[string][]agentapi.Message
	Recent      []agentapi.Message // RecentHubMessages 返回
	Sent        []string // Send 调用记录 "chatID|text"
	FailSendOn  string   // Send 对该 chatID 返回错误
	Stopped     []string
	Recalled    []int64
	ToolsV      []agentapi.Option
	WorkspacesV []agentapi.Option
	// WorkspaceSessionsV 是 WorkspaceSessions 的预制数据：工作区路径 → 会话列表。
	WorkspaceSessionsV map[string][]agentapi.Session
	Created     [][3]string
	Opened      []string
	FeedV       *agentapi.Feed // 惰性初始化，经 Feed() 访问
}

func (f *FakeBackend) ActiveSessions() []agentapi.Session { return f.Active }
func (f *FakeBackend) DiskSessions() []agentapi.Session   { return f.Disk }
func (f *FakeBackend) ArchivedIDs() []string              { return f.Archived }
func (f *FakeBackend) SessionMessages(chatID string) []agentapi.Message {
	return f.Msgs[chatID]
}

// RecentHubMessages 返回预制的最近消息；n<=0 返回空（与真实实现同口径）。
func (f *FakeBackend) RecentHubMessages(n int) []agentapi.Message {
	if n <= 0 || len(f.Recent) == 0 {
		return nil
	}
	if len(f.Recent) > n {
		return f.Recent[len(f.Recent)-n:]
	}
	return f.Recent
}

func (f *FakeBackend) Send(chatID, text string) error {
	if f.FailSendOn == chatID {
		return fmt.Errorf("err.chat.gone")
	}
	f.Sent = append(f.Sent, chatID+"|"+text)
	return nil
}
func (f *FakeBackend) Stop(chatID string) error {
	f.Stopped = append(f.Stopped, chatID)
	return nil
}
func (f *FakeBackend) Recall(msgID int64) error {
	f.Recalled = append(f.Recalled, msgID)
	return nil
}
func (f *FakeBackend) ListModels(chatID string) ([]agentapi.ModelOption, string, error) {
	return []agentapi.ModelOption{{ID: "m1", Name: "Model 1"}}, "m1", nil
}
func (f *FakeBackend) SetModel(chatID, model string) error { return nil }
func (f *FakeBackend) GrantPermission(chatID, reqID string, allow bool) error {
	return nil
}
func (f *FakeBackend) Create(toolID, wsID, model string) (agentapi.Session, error) {
	f.Created = append(f.Created, [3]string{toolID, wsID, model})
	return agentapi.Session{ID: "chat-new", SessionID: "sess-new", Tool: toolID, Workspace: wsID, Status: agentapi.StatusRunning}, nil
}
func (f *FakeBackend) Open(sessionID string) (agentapi.Session, error) {
	f.Opened = append(f.Opened, sessionID)
	if sessionID == "missing" {
		return agentapi.Session{}, errors.New("err.hub.acp_unsupported")
	}
	return agentapi.Session{ID: "chat-opened", SessionID: sessionID, Status: agentapi.StatusRunning}, nil
}
func (f *FakeBackend) Tools() []agentapi.Option      { return f.ToolsV }
func (f *FakeBackend) Workspaces() []agentapi.Option { return f.WorkspacesV }

// WorkspaceSessions 按预制的 工作区→会话 列表返回前 n 个（列表顺序即返回
// 顺序，由测试数据保证新→旧）。
func (f *FakeBackend) WorkspaceSessions(wsPath string, n int) []agentapi.Session {
	all := f.WorkspaceSessionsV[wsPath]
	if n <= 0 {
		return nil
	}
	if len(all) > n {
		return all[:n]
	}
	return all
}
func (f *FakeBackend) Feed() *agentapi.Feed {
	if f.FeedV == nil {
		f.FeedV = agentapi.NewFeed()
	}
	return f.FeedV
}

// NewFake 构造标准桩：1 个活跃会话（带两条消息）+ 1 个磁盘会话 + 候选。
func NewFake() *FakeBackend {
	return &FakeBackend{
		Active: []agentapi.Session{
			{ID: "chat-a", SessionID: "sess-a", Tool: "claude", Title: "甲", Workspace: "d:/ws/a", Status: agentapi.StatusRunning, LastTs: 200, Summary: "最近回复"},
		},
		Disk: []agentapi.Session{
			{SessionID: "sess-c", Tool: "gemini", Title: "丙", Workspace: "d:/ws/c", Status: agentapi.StatusDisk},
		},
		Msgs: map[string][]agentapi.Message{
			"chat-a": {
				{ID: 1, SessionID: "chat-a", Kind: "user", Text: "开始", Ts: 100},
				{ID: 2, SessionID: "chat-a", Kind: "agent", Text: "回复", Ts: 200},
			},
		},
		ToolsV:      []agentapi.Option{{ID: "claude", Label: "Claude Code"}},
		WorkspacesV: []agentapi.Option{{ID: "d:/ws/a", Label: "a"}},
	}
}
