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
	"github.com/yangk/kshell/internal/launch"
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
	mu       sync.Mutex
	conns    []chat.Conn
}

func (b *flakyBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	if b.failNext.Load() {
		return nil, errors.New("起进程失败")
	}
	c := &fakeChatConn{waitCh: make(chan struct{})}
	b.mu.Lock()
	b.conns = append(b.conns, c)
	b.mu.Unlock()
	return c, nil
}

func (b *flakyBackend) connAt(i int) chat.Conn {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conns[i]
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

// TestEnsureChatAliveOpenFailureNotReady 重建失败返回 err.chat.rebuild_failed 语义（细节透出）。
func TestEnsureChatAliveOpenFailureNotReady(t *testing.T) {
	b := &flakyBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id := opened.Chat.ID
	exitChatConn(t, m, id, b.connAt(0))
	b.failNext.Store(true)

	if _, err := app.ensureChatAlive(id); err == nil || err.Error() != "err.chat.rebuild_failed|起进程失败" {
		t.Fatalf("want err.chat.rebuild_failed|..., got %v", err)
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

// TestRebuildChatEmptyWorkspaceFails 工作区为空时不再合成空 path 的 launch，
// 直接报 err.chat.rebuild_failed（细节透出，前端可提示具体原因）。
func TestRebuildChatEmptyWorkspaceFails(t *testing.T) {
	app := newReactivateTestApp(t, &recordingBackend{})
	m := app.chats()
	_, err := app.rebuildChat(m, chat.Info{ID: "cx_1", Key: "new:1", Kind: chat.KindNew})
	if err == nil || err.Error() != "err.chat.rebuild_failed|workspace empty" {
		t.Fatalf("want err.chat.rebuild_failed|workspace empty, got %v", err)
	}
}

// TestRebuildChatNotLoggedInBareWireKey 重建时工具已登出：返回裸 wire key
// err.launch.not_logged_in（前端可直译并引导去登录），不得包成
// err.chat.rebuild_failed|... 复合错误。
func TestRebuildChatNotLoggedInBareWireKey(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenWorkspaceACP(`D:\w`, "claude")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	// 登出：重建路径的 launch 门禁据此拒绝
	app.mu.Lock()
	for i := range app.tools {
		if app.tools[i].ID == "claude" {
			app.tools[i].Auth = string(providers.AuthOut)
		}
	}
	app.mu.Unlock()

	_, err = app.ensureChatAlive(oldID)
	if err == nil || err.Error() != launch.ErrNotLoggedIn.Error() {
		t.Fatalf("未登录重建应返回裸 %q, got %v", launch.ErrNotLoggedIn.Error(), err)
	}
	if b.count() != 1 {
		t.Fatalf("未登录不应起新连接, got %d", b.count())
	}
}

// TestRebuildChatSessionFallbackToWorkspace resume 降级路径：KindSession 有
// SessionID 但 discovery 里查不到该磁盘会话（如缓存被清）→ 按工作区新起，
// 宁可丢上下文也不报错卡死。
func TestRebuildChatSessionFallbackToWorkspace(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	// 直接构造一个指向「磁盘上不存在的会话」的 Info，绕过正常重建入口。
	info, _ := m.Get(oldID)
	_ = m.Close(oldID)
	newID, err := app.rebuildChat(m, chat.Info{
		ID:        oldID,
		Key:       info.Key,
		Kind:      chat.KindSession,
		SessionID: "gone-session",
		Workspace: info.Workspace,
		ToolID:    info.ToolID,
	})
	if err != nil {
		t.Fatalf("降级重建不应失败: %v", err)
	}
	if newID == oldID {
		t.Fatalf("降级重建应产生新 ID, got %s", newID)
	}
	if got := b.connAt(1).loadedIDs(); len(got) != 0 {
		t.Fatalf("降级按工作区新起不应 LoadSession, got %v", got)
	}
	got, ok := m.Get(newID)
	if !ok || got.Kind != chat.KindNew {
		t.Fatalf("降级重建应保持 KindNew: ok=%v info=%+v", ok, got)
	}
	// 别名链应覆盖降级路径：旧 ID 仍可解析到新会话
	if resolved, ok := app.resolveChatID(oldID); !ok || resolved != newID {
		t.Fatalf("降级后旧 ID 应解析到新会话: got %s ok=%v", resolved, ok)
	}
}

// TestRebuiltChatEmitsOriginalID 集成：重建后 manager 的更新事件经出站翻译
// 带回旧 ID——前端按旧 ID 索引的状态才收得到（Critical 修复点）。
func TestRebuiltChatEmitsOriginalID(t *testing.T) {
	b := &recordingBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, b)
	app := env.app
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))

	// 触发重建（第二条消息经旧 ID 投递），等新会话推 turn_done。
	if err := app.SendChatPrompt(oldID, "第一条"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, ev := range env.events.all() {
			if ev.name != "chat:update" {
				continue
			}
			if id, _ := ev.payload["id"].(string); id == oldID {
				if u, ok := ev.payload["update"].(chat.Update); ok && u.Type == "turn_done" {
					found = true
				}
			}
		}
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("重建后未收到带旧 ID 的 chat:update(turn_done)")
}

// TestOldIDHitsNewSessionViaBindings 前端持旧 ID 调 ChatHistory / CancelChat /
// ListChatModels 应命中重建后的新会话（入站别名解析）。
func TestOldIDHitsNewSessionViaBindings(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))
	if err := app.SendChatPrompt(oldID, "唤醒"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	waitPromptText(t, b.connAt(1), "唤醒")

	// 旧 ID 读历史：应返回非空（含刚投递的 user 消息），证明命中新会话
	hist := app.ChatHistory(oldID)
	if len(hist) == 0 {
		t.Fatal("旧 ID 调 ChatHistory 应命中重建后的新会话")
	}
	// 旧 ID 取模型视图：不报错即视为命中（fake conn 会话无模型信息时回空视图）
	if _, err := app.ListChatModels(oldID); err != nil {
		t.Fatalf("旧 ID 调 ListChatModels: %v", err)
	}
	// 旧 ID 取消：manager 里只有新 ID，Cancel 命中返回 nil；未解析时 errNotFound
	if err := app.CancelChat(oldID); err != nil {
		t.Fatalf("旧 ID 调 CancelChat: %v", err)
	}
}

// TestCloseChatClearsAliasChain 重建后关闭新会话：别名整链清理，
// 再向旧 ID 发送必须报 err.chat.gone（不静默复活）。
func TestCloseChatClearsAliasChain(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))
	if err := app.SendChatPrompt(oldID, "唤醒"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	newID, ok := app.resolveChatID(oldID)
	if !ok || newID == oldID {
		t.Fatalf("重建后应能解析出新 ID: %s ok=%v", newID, ok)
	}

	if err := app.CloseChat(newID); err != nil {
		t.Fatalf("CloseChat: %v", err)
	}
	// 旧 ID 再发送：别名已清、manager 里也查不到 → err.chat.gone
	err = app.SendChatPrompt(oldID, "不该送达")
	if err == nil || err.Error() != "err.chat.gone" {
		t.Fatalf("关链后旧 ID 发送应报 err.chat.gone, got %v", err)
	}
}

// TestOriginalChatIDChain 链式重建（旧→新→更新）时出站翻译回溯到链头。
func TestOriginalChatIDChain(t *testing.T) {
	app := newReactivateTestApp(t, &recordingBackend{})
	app.rememberChatAlias("c1", "c2")
	app.rememberChatAlias("c2", "c3")
	if got := app.originalChatID("c3"); got != "c1" {
		t.Fatalf("链尾应翻回链头 c1, got %s", got)
	}
	if got := app.originalChatID("c1"); got != "c1" {
		t.Fatalf("链头应原样返回, got %s", got)
	}
	if got := app.originalChatID("unrelated"); got != "unrelated" {
		t.Fatalf("无别名应原样返回, got %s", got)
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

// TestListChatsTranslatesToOriginalID 重建后 ListChats 只含旧 ID：前端镜像按 ID
// 整表映射（scan:done 后 setChats），manager 侧新 ID 不翻回链头的话旧 ID 行会被
// 当陌生行丢弃，重建过的页签直接消失。
func TestListChatsTranslatesToOriginalID(t *testing.T) {
	b := &recordingBackend{}
	app := newReactivateTestApp(t, b)
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))
	if err := app.SendChatPrompt(oldID, "唤醒"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	waitPromptText(t, b.connAt(1), "唤醒")

	ids := map[string]bool{}
	for _, c := range app.ListChats() {
		ids[c.ID] = true
	}
	if len(ids) != 1 || !ids[oldID] {
		t.Fatalf("重建后 ListChats 应只含旧 ID %s, got %v", oldID, ids)
	}
}

// TestRebuildEmitsChatMetaRebuilt resume 重建后应 emit chat:meta(Rebuilt=true) 且
// ID 翻回旧 ID：前端据此重置 Seq 门控并重拉 ChatHistory（重放基准换血）。
func TestRebuildEmitsChatMetaRebuilt(t *testing.T) {
	b := &recordingBackend{}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, b)
	app := env.app
	m := app.chats()

	opened, err := app.OpenSessionACP("s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	oldID := opened.Chat.ID
	exitChatConn(t, m, oldID, b.connAt(0))
	if err := app.SendChatPrompt(oldID, "唤醒"); err != nil {
		t.Fatalf("SendChatPrompt: %v", err)
	}
	waitPromptText(t, b.connAt(1), "唤醒")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, ev := range env.events.all() {
			if ev.name != "chat:meta" {
				continue
			}
			info, ok := ev.raw.(chat.Info)
			if !ok || info.ID != oldID {
				continue
			}
			if info.Rebuilt {
				found = true
			}
		}
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("重建后未收到 chat:meta(旧ID, Rebuilt=true)")
}
