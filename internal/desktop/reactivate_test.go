package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/providers"
)

// recordingConn 在 fakeChatConn 基础上记录 LoadSession / Prompt 调用，
// 供重建（resume）与投递断言。
type recordingConn struct {
	fakeChatConn
	mu      sync.Mutex
	loaded  []string
	prompts [][]acp.ContentBlock
}

func (c *recordingConn) LoadSession(ctx context.Context, sessionID, cwd string) (acp.SessionStart, error) {
	c.mu.Lock()
	c.loaded = append(c.loaded, sessionID)
	c.mu.Unlock()
	return c.fakeChatConn.LoadSession(ctx, sessionID, cwd)
}

func (c *recordingConn) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, blocks)
	c.mu.Unlock()
	return c.fakeChatConn.Prompt(ctx, sessionID, blocks)
}

func (c *recordingConn) loadedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.loaded...)
}

func (c *recordingConn) promptTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.prompts))
	for _, blocks := range c.prompts {
		text := ""
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				text = b.Text
				break
			}
		}
		out = append(out, text)
	}
	return out
}

// recordingBackend 记录创建的连接（断言重建次数 / 取最新连接）。
type recordingBackend struct {
	mu    sync.Mutex
	conns []*recordingConn
}

func (b *recordingBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	c := &recordingConn{fakeChatConn: fakeChatConn{waitCh: make(chan struct{})}}
	b.mu.Lock()
	b.conns = append(b.conns, c)
	b.mu.Unlock()
	return c, nil
}

func (b *recordingBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.conns)
}

func (b *recordingBackend) connAt(i int) *recordingConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conns[i]
}

// flakyBackend 前几次成功、置位后起进程失败（模拟重建失败路径）。
type flakyBackend struct {
	failNext atomic.Bool
	conns    []chat.Conn
}

func (b *flakyBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	if b.failNext.Load() {
		return nil, errors.New("起进程失败")
	}
	c := &fakeChatConn{waitCh: make(chan struct{})}
	b.conns = append(b.conns, c)
	return c, nil
}

// newReactivateTestApp 装配带桩后端的 App（与 newChatTestApp 同构，便于注入自定义 backend）。
func newReactivateTestApp(t *testing.T, backend chat.Backend) *App {
	t.Helper()
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, backend)
	return env.app
}

// exitChatConn 关闭底层连接模拟进程自然退出，并等 manager 标记 exited。
func exitChatConn(t *testing.T, m *chat.Manager, id string, c chat.Conn) {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.Get(id); ok && info.Status == chat.StatusExited {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("会话未进入 exited 状态")
}

// TestEnsureChatAliveReactivatesExitedSession 已退出的会话聊天应按原 sessionID 重建（resume）。
func TestEnsureChatAliveReactivatesExitedSession(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	newID, err := app.ensureChatAlive(oldID)
	if err != nil {
		t.Fatalf("ensureChatAlive: %v", err)
	}
	if newID == oldID {
		t.Fatalf("重建后应返回新 ID, got %s", newID)
	}
	if b.count() != 2 {
		t.Fatalf("应重建一次连接, got %d", b.count())
	}
	if got := b.connAt(1).loadedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("重建应 LoadSession(原会话), got %v", got)
	}
	info, ok := m.Get(newID)
	if !ok {
		t.Fatalf("新会话应在册")
	}
	if info.Kind != chat.KindSession || info.SessionID != "s1" || info.Status != chat.StatusReady {
		t.Fatalf("重建后 Info 不符: %+v", info)
	}
}

// TestEnsureChatAliveReadyIsNoop 未退出的聊天走快速路径，不触发重建。
func TestEnsureChatAliveReadyIsNoop(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id := opened.Chat.ID
	got, err := app.ensureChatAlive(id)
	if err != nil {
		t.Fatalf("ensureChatAlive: %v", err)
	}
	if got != id {
		t.Fatalf("未重建应原样返回 ID, got %s", got)
	}
	if b.count() != 1 {
		t.Fatalf("ready 聊天不应重建, got %d 次连接", b.count())
	}
}

// TestEnsureChatAliveUnknownIDGone 未知 ID 返回 err.chat.gone 语义。
func TestEnsureChatAliveUnknownIDGone(t *testing.T) {
	app := newReactivateTestApp(t, &recordingBackend{})
	_, err := app.ensureChatAlive("nope")
	if err == nil || err.Error() != "err.chat.gone" {
		t.Fatalf("want err.chat.gone, got %v", err)
	}
}

// TestEnsureChatAliveOpenFailureNotReady 重建失败返回 err.chat.not_ready 语义。
func TestEnsureChatAliveOpenFailureNotReady(t *testing.T) {
	b := &flakyBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id := opened.Chat.ID
	exitChatConn(t, m, id, b.conns[0])
	b.failNext.Store(true)

	if _, err := app.ensureChatAlive(id); err == nil || err.Error() != "err.chat.not_ready|起进程失败" {
		t.Fatalf("want err.chat.not_ready|..., got %v", err)
	}
}

// TestSendChatPromptReactivatesAndDelivers 集成：向已退出聊天发送消息自动重建并投递；
// 前端持旧 ID 的后续发送经别名解析继续可用。
func TestSendChatPromptReactivatesAndDelivers(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	if err := app.SendChatPrompt(oldID, "继续修"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	if b.count() != 2 {
		t.Fatalf("发送应触发重建, got %d 次连接", b.count())
	}
	waitPromptText(t, b.connAt(1), "继续修")

	// 第二条消息仍用旧 ID（前端页签不知道新 ID）：经别名解析应继续投递
	if err := app.SendChatPrompt(oldID, "再查一下"); err != nil {
		t.Fatalf("第二条 SendChatPrompt: %v", err)
	}
	waitPromptText(t, b.connAt(1), "再查一下")
}

// TestEnsureChatAliveRebuildsNewChatByWorkspace 未绑定会话的新建聊天按原工作区+工具重建。
func TestEnsureChatAliveRebuildsNewChatByWorkspace(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenWorkspaceACP(`D:\w`, "claude")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	newID, err := app.ensureChatAlive(oldID)
	if err != nil {
		t.Fatalf("ensureChatAlive: %v", err)
	}
	if newID == oldID || b.count() != 2 {
		t.Fatalf("应重建: newID=%s conns=%d", newID, b.count())
	}
	info, ok := m.Get(newID)
	if !ok {
		t.Fatal("新会话应在册")
	}
	if info.Kind != chat.KindNew || info.SessionID != "" {
		t.Fatalf("新建聊天重建应保持 KindNew: %+v", info)
	}
	// 新起路径不走 LoadSession
	if got := b.connAt(1).loadedIDs(); len(got) != 0 {
		t.Fatalf("新起不应 LoadSession, got %v", got)
	}
}

func waitPromptText(t *testing.T, c *recordingConn, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range c.promptTexts() {
			if got == text {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时未等到 prompt %q, got %v", text, c.promptTexts())
}
