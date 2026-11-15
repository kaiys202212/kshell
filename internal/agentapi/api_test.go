package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/hubtools"
)

// fakeBackend 是行为测试共用的 Backend 桩：记录调用参数、返回预制数据。
type fakeBackend struct {
	active     []Session
	disk       []Session
	archived   []string
	msgs       map[string][]Message // chatID → 消息
	recent     []Message            // RecentHubMessages 返回
	sent       []string             // Send 调用记录 "chatID|text"
	failSendOn string               // Send 对该 chatID 返回错误（测试失败路径）
	stopped    []string
	recalled   []int64
	models     []ModelOption
	curModel   string
	setModels  [][2]string // [chatID, model]
	grants     []grantCall
	created    [][3]string // [toolID, wsID, model]
	opened     []string
	tools      []Option
	workspaces []Option
	feed       *Feed
}

type grantCall struct {
	chatID string
	reqID  string
	allow  bool
}

func (f *fakeBackend) ActiveSessions() []Session { return f.active }
func (f *fakeBackend) DiskSessions() []Session   { return f.disk }
func (f *fakeBackend) ArchivedIDs() []string     { return f.archived }

func (f *fakeBackend) SessionMessages(chatID string) []Message { return f.msgs[chatID] }

// RecentHubMessages 模拟 hub 取尾 n 条（n<=0 返回空，与真实实现同口径）。
func (f *fakeBackend) RecentHubMessages(n int) []Message {
	if n <= 0 || len(f.recent) == 0 {
		return nil
	}
	if len(f.recent) > n {
		return f.recent[len(f.recent)-n:]
	}
	return f.recent
}

func (f *fakeBackend) Send(chatID, text string) error {
	if f.failSendOn == chatID {
		return errors.New("err.chat.gone")
	}
	f.sent = append(f.sent, chatID+"|"+text)
	return nil
}
func (f *fakeBackend) Stop(chatID string) error {
	f.stopped = append(f.stopped, chatID)
	return nil
}
func (f *fakeBackend) Recall(msgID int64) error {
	f.recalled = append(f.recalled, msgID)
	return nil
}
func (f *fakeBackend) ListModels(chatID string) ([]ModelOption, string, error) {
	return f.models, f.curModel, nil
}
func (f *fakeBackend) SetModel(chatID, model string) error {
	f.setModels = append(f.setModels, [2]string{chatID, model})
	return nil
}
func (f *fakeBackend) GrantPermission(chatID, reqID string, allow bool) error {
	f.grants = append(f.grants, grantCall{chatID, reqID, allow})
	return nil
}
func (f *fakeBackend) Create(toolID, wsID, model string) (Session, error) {
	f.created = append(f.created, [3]string{toolID, wsID, model})
	return Session{ID: "chat-new", SessionID: "sess-new", Tool: toolID, Workspace: wsID, Status: StatusRunning}, nil
}
func (f *fakeBackend) Open(sessionID string) (Session, error) {
	f.opened = append(f.opened, sessionID)
	if sessionID == "missing" {
		return Session{}, errors.New("err.hub.acp_unsupported")
	}
	return Session{ID: "chat-opened", SessionID: sessionID, Status: StatusRunning}, nil
}
func (f *fakeBackend) Tools() []Option      { return f.tools }
func (f *fakeBackend) Workspaces() []Option { return f.workspaces }
func (f *fakeBackend) Feed() *Feed {
	if f.feed == nil {
		f.feed = NewFeed()
	}
	return f.feed
}

// 编译期保证桩覆盖完整接口。
var _ Backend = (*fakeBackend)(nil)

// newFake 构造带标准数据的桩：两个活跃会话（一个 running 一个 perm）、
// 两个磁盘会话（其一已归档）。
func newFake() *fakeBackend {
	f := &fakeBackend{
		active: []Session{
			{ID: "chat-a", SessionID: "sess-a", Tool: "claude", Title: "修复登录", Workspace: "d:/ws/a", Status: StatusRunning},
			{ID: "chat-b", SessionID: "sess-b", Tool: "codex", Title: "写文档", Workspace: "d:/ws/b", Status: StatusPerm, LastTs: 200, Summary: "等待授权"},
		},
		disk: []Session{
			{SessionID: "sess-c", Tool: "gemini", Title: "重构模块", Workspace: "d:/ws/c", Status: StatusDisk},
			{SessionID: "sess-d", Tool: "claude", Title: "旧任务", Workspace: "d:/ws/d", Status: StatusDisk},
		},
		archived: []string{"sess-d"},
		msgs:     map[string][]Message{},
		tools: []Option{
			{ID: "claude", Label: "Claude Code"},
			{ID: "codex", Label: "Codex"},
		},
		workspaces: []Option{
			{ID: "d:/ws/a", Label: "a"},
			{ID: "d:/ws/b", Label: "b"},
		},
	}
	return f
}

func TestNew(t *testing.T) {
	api := New(newFake())
	if api == nil {
		t.Fatal("New 返回 nil")
	}
	// UpdateLLM 可调用且写入配置。
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	if api.llmCfg().Model != "m" {
		t.Fatalf("UpdateLLM 未生效: %+v", api.llmCfg())
	}
}

func TestChatNotReady(t *testing.T) {
	api := New(newFake())
	_, err := api.chat(context.Background(), nil)
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("未配置 LLM 时期望 ErrLLMNotReady，得到 %v", err)
	}
}
