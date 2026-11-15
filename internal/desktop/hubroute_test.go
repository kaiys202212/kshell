package desktop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
)

// routeFakeExecutor 是命令层 Executor 的桌面测试替身：只记录 Send 与
// SessionRecentMessages，其余动作空实现。Console 用真实 hub.Console，
// 只把执行后端换成它，以便直接断言投递目标与文本。
type routeFakeExecutor struct {
	list     []hub.SessionRef
	statuses []hub.SessionStatus
	recent   map[string][]hub.Message // chatID → SessionRecentMessages 返回
	lastN    int                      // 最近一次 SessionRecentMessages 的条数

	sendCalls [][2]string
}

func (f *routeFakeExecutor) ListSessions() []hub.SessionRef { return f.list }

func (f *routeFakeExecutor) LastActivity() (int64, hub.SessionRef, bool) {
	return 0, hub.SessionRef{}, false
}

func (f *routeFakeExecutor) SwitchTo(string) error { return nil }

func (f *routeFakeExecutor) OpenSession(sessionID string) (hub.SessionRef, error) {
	for _, r := range f.list {
		if r.SessionID == sessionID {
			return r, nil
		}
	}
	return hub.SessionRef{}, fmt.Errorf("err.hub.no_session")
}

func (f *routeFakeExecutor) RecoverSession(string) (hub.SessionRef, bool) {
	return hub.SessionRef{}, false
}

func (f *routeFakeExecutor) EasyNew() (hub.SessionRef, error) { return hub.SessionRef{}, nil }

func (f *routeFakeExecutor) NewMenuOptions() (tools, workspaces, models []hub.MenuOption) {
	return nil, nil, nil
}

func (f *routeFakeExecutor) CreateNew(string, string, string) (hub.SessionRef, error) {
	return hub.SessionRef{}, nil
}

func (f *routeFakeExecutor) Stop(string) error { return nil }

func (f *routeFakeExecutor) ListModels(string) ([]hub.MenuOption, string, error) {
	return nil, "", nil
}

func (f *routeFakeExecutor) SetModel(string, string) error { return nil }

func (f *routeFakeExecutor) SearchSessions(string) []hub.SessionRef { return nil }

func (f *routeFakeExecutor) Send(chatID, text string) error {
	f.sendCalls = append(f.sendCalls, [2]string{chatID, text})
	return nil
}

func (f *routeFakeExecutor) GrantPermission(string, string, bool) error { return nil }

func (f *routeFakeExecutor) SessionStatuses() []hub.SessionStatus { return f.statuses }

func (f *routeFakeExecutor) Status() hub.ConsoleStatus { return hub.ConsoleStatus{} }

func (f *routeFakeExecutor) RecentMessages(int) []hub.Message { return nil }

func (f *routeFakeExecutor) SessionRecentMessages(chatID string, n int) []hub.Message {
	f.lastN = n
	return f.recent[chatID]
}

// routeFixture 装配 fake App（真实 chat manager 提供 3 个活跃会话，供
// ListSessions 编号映射与捷径路径复用）+ 真 hub.Console + fake Executor。
type routeFixture struct {
	app  *App
	c    *hub.Console
	ex   *routeFakeExecutor
	refs []hub.SessionRef
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	app := wxApp(t, nil)

	m := newChatManagerWith(func(string, ...any) {}, fakeChatBackend{}, nil, nil, nil, nil, nil)
	t.Cleanup(m.CloseAll)
	opens := []struct{ key, sid, ws, title string }{
		{"session:s1", "s1", "D:/w1", "甲"},
		{"session:s2", "s2", "D:/w2", "乙"},
		{"session:s3", "s3", "D:/w3", "丙"},
	}
	for _, o := range opens {
		if _, err := m.Open(o.key, chat.Info{
			Kind: chat.KindSession, SessionID: o.sid, Workspace: o.ws, Title: o.title, ToolID: "claude",
		}, chat.Spec{Path: "x"}, o.sid); err != nil {
			t.Fatalf("open %s: %v", o.key, err)
		}
	}

	app.mu.Lock()
	app.opts.Chats = m
	app.opts.Config = config.Default()
	app.opts.Config.Language = "zh-CN"
	app.mu.Unlock()

	refs := newWXExecutor(app).ListSessions()
	if len(refs) != 3 {
		t.Fatalf("应有 3 个会话, got %d: %+v", len(refs), refs)
	}

	ex := &routeFakeExecutor{list: append([]hub.SessionRef(nil), refs...)}
	c := hub.NewConsoleLang(ex, "zh-CN")
	app.mu.Lock()
	app.hubConsole = c
	app.mu.Unlock()
	return &routeFixture{app: app, c: c, ex: ex, refs: refs}
}

// TestRouteBriefsRealStates 覆盖 routeBriefs 的真实状态映射：statuses 命中的
// 行用真实 State，未命中（磁盘会话无活跃 chat）保留兜底（终端占用 gone /
// 其余 idle），编号保持 ListSessions 顺序不重排。routeBriefs 供 weixin.go 的
// RouteInput 降级路径使用（desktop 主路径已换管理者循环）。
func TestRouteBriefsRealStates(t *testing.T) {
	refs := []hub.SessionRef{
		{ChatID: "c1", SessionID: "s1", Key: "k1", Tool: "claude", Title: "甲", TerminalActive: true},
		{ChatID: "c2", SessionID: "s2", Key: "k2", Tool: "codex", Title: "乙"},
		{ChatID: "c3", SessionID: "s3", Key: "k3", Tool: "gemini", Title: "丙"},
		{ChatID: "c4", SessionID: "s4", Key: "k4", Tool: "cursor", Title: "丁"},
		{Tool: "claude", Title: "磁盘会话（无 ChatID）", TerminalActive: true},
	}
	statuses := []hub.SessionStatus{
		{Ref: hub.SessionRef{ChatID: "c1", SessionID: "s1"}, State: "running"},
		{Ref: hub.SessionRef{ChatID: "c2", SessionID: "s2"}, State: "perm"},
		{Ref: hub.SessionRef{ChatID: "c3", SessionID: "s3"}, State: "idle"},
	}
	got := routeBriefs(refs, statuses)
	if len(got) != len(refs) {
		t.Fatalf("briefs 条数 = %d, 期望 %d", len(got), len(refs))
	}
	wantState := []string{"running", "perm", "idle", "idle", "gone"}
	// 第 4 个（c4）不在 statuses：兜底 idle；第 5 个磁盘会话终端占用 → gone。
	for i, w := range wantState {
		if got[i].Status != w {
			t.Errorf("brief[%d].Status = %q, 期望 %q", i, got[i].Status, w)
		}
		if got[i].Num != i+1 {
			t.Errorf("brief[%d].Num = %d, 期望 %d（不重排）", i, got[i].Num, i+1)
		}
		if got[i].Title != refs[i].Title {
			t.Errorf("brief[%d].Title = %q, 期望 %q", i, got[i].Title, refs[i].Title)
		}
	}
	if got[0].Tool != "claude" {
		t.Errorf("brief[0].Tool = %q", got[0].Tool)
	}
}

// setPermSession 向 hub store 追加一条 Notification 事件 agent 消息，使
// wxExecutor.SessionStatuses 把该会话判为 perm（等待用户）。捷径与循环的
// 等待会话判定同源（State=="perm"，不引入问号启发）。
func setPermSession(t *testing.T, fx *routeFixture, ref hub.SessionRef) {
	t.Helper()
	store := fx.app.snapshot().HubStore
	if store == nil {
		t.Fatal("fixture 应装配 HubStore")
	}
	if err := store.Append(hub.Message{
		ChatID: ref.ChatID, Kind: "agent", Event: "Notification", Text: "等待权限确认",
	}); err != nil {
		t.Fatalf("append perm message: %v", err)
	}
}

// injectHubRunner 注入管理者循环桩并返回调用计数指针。
func injectHubRunner(fx *routeFixture, fn func(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error)) *int {
	calls := 0
	fx.app.mu.Lock()
	fx.app.hubRunner = func(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
		calls++
		return fn(ctx, input, opts)
	}
	fx.app.mu.Unlock()
	return &calls
}

// hubAskPendingOf 读取 desktop 续轮衔接标记（mu 保护）。
func hubAskPendingOf(a *App) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hubAskPending
}

// lastUserMessage 取 hub store 里最后一条指定 ChatID 的用户消息（无则 ok=false）。
func lastUserMessage(t *testing.T, fx *routeFixture, chatID string) (hub.Message, bool) {
	t.Helper()
	store := fx.app.snapshot().HubStore
	if store == nil {
		t.Fatal("fixture 应装配 HubStore")
	}
	var found hub.Message
	ok := false
	for _, m := range store.List() {
		if m.Kind == "user" && m.ChatID == chatID {
			found = m
			ok = true
		}
	}
	return found, ok
}

// TestHubSmartSend 覆盖桌面 Hub 管理者循环入口：回执回显/续轮衔接/降级回落。
func TestHubSmartSend(t *testing.T) {
	t.Run("循环成功回执回显", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"已把任务发给「甲」。"}, ToolSteps: 2}, nil
		})
		view, err := fx.app.HubSmartSend("req", "把构建任务发给修复登录那个会话")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending {
			t.Fatal("循环成功应 Pending=false")
		}
		if *calls != 1 {
			t.Fatalf("runner 调用 = %d, 期望 1", *calls)
		}
		if len(view.Replies) != 1 || view.Replies[0].Text != "已把任务发给「甲」。" {
			t.Fatalf("回执 = %+v", view.Replies)
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("desktop 主路径不直接投递（循环内 send 走 Backend）: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("AskPending 回执即选项列表，短输入续轮", func(t *testing.T) {
		fx := newRouteFixture(t)
		injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"要发给哪个会话？\n1. 甲\n2. 乙"}, AskPending: true}, nil
		})
		view, err := fx.app.HubSmartSend("req", "发一下")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending {
			t.Fatal("AskPending 应 Pending=false（选项列表就是回执）")
		}
		if !hubAskPendingOf(fx.app) {
			t.Fatal("AskPending 结果应记录进续轮衔接标记")
		}

		// 长输入：新需求，不传 Continuation（agentapi 内部清 pending 开新轮）。
		var gotOpts agentapi.RunOpts
		fx.app.mu.Lock()
		fx.app.hubRunner = func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			gotOpts = opts
			return agentapi.RunResult{Replies: []string{"好的"}}, nil
		}
		fx.app.mu.Unlock()
		long := strings.Repeat("这", 30)
		if _, err := fx.app.HubSmartSend("req", long); err != nil {
			t.Fatalf("长输入 HubSmartSend: %v", err)
		}
		if gotOpts.Continuation {
			t.Fatal("长输入不应传 Continuation=true")
		}
		if hubAskPendingOf(fx.app) {
			t.Fatal("非 ask 结果应清零续轮衔接标记")
		}

		// 再次 ask 后短输入：Continuation=true 续轮衔接。
		fx.app.mu.Lock()
		fx.app.hubRunner = func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"要发给哪个会话？\n1. 甲"}, AskPending: true}, nil
		}
		fx.app.mu.Unlock()
		if _, err := fx.app.HubSmartSend("req", "再问一次"); err != nil {
			t.Fatalf("ask HubSmartSend: %v", err)
		}
		fx.app.mu.Lock()
		fx.app.hubRunner = func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			gotOpts = opts
			return agentapi.RunResult{Replies: []string{"完成"}}, nil
		}
		fx.app.mu.Unlock()
		if _, err := fx.app.HubSmartSend("req", "1"); err != nil {
			t.Fatalf("短输入 HubSmartSend: %v", err)
		}
		if !gotOpts.Continuation {
			t.Fatal("AskPending 语境下的短输入应传 Continuation=true")
		}
		if gotOpts.Channel != "hub" {
			t.Fatalf("Channel = %q, 期望 hub", gotOpts.Channel)
		}
	})

	t.Run("ErrLoopBusy 回落待选", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{}, agentapi.ErrLoopBusy
		})
		view, err := fx.app.HubSmartSend("req", "原文不能丢")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("ErrLoopBusy 应 Pending=true 交前端回落待选")
		}
		if *calls != 1 {
			t.Fatalf("runner 调用 = %d, 期望 1", *calls)
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("模型未配（默认循环）回落待选", func(t *testing.T) {
		fx := newRouteFixture(t) // 未注入 hubRunner，走真实 AgentAPI().Run
		view, err := fx.app.HubSmartSend("req", "没配模型")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("模型未配（ErrLLMNotReady）应 Pending=true")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("逃生门命令不进循环", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应到达"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "#g 1 hi")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("逃生门应 Pending=true 交前端走命令路径")
		}
		if *calls != 0 {
			t.Fatalf("逃生门不应调循环, calls=%d", *calls)
		}
	})

	t.Run("explain 不再命中逃生门（走循环）", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"解释如下"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "explain 一下这段报错")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending || len(view.Replies) != 1 {
			t.Fatalf("explain 应进循环并回执: %+v", view)
		}
		if *calls != 1 {
			t.Fatalf("循环调用 = %d, 期望 1", *calls)
		}
	})

	t.Run("待选菜单挂起时让路", func(t *testing.T) {
		fx := newRouteFixture(t)
		// 先建立待选菜单（Busy=true）。
		if pick, err := fx.app.HubPickTarget("先选目标"); err != nil || !pick.Pending {
			t.Fatalf("fixture 应能建立待选菜单: view=%+v err=%v", pick, err)
		}
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应到达"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "菜单挂起中的文本")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("Busy 应 Pending=true")
		}
		if *calls != 0 {
			t.Fatalf("Busy 不应调循环, calls=%d", *calls)
		}
	})
}

// TestHubSmartSendShortcut 覆盖「继续类短确认」确定性捷径：唯一等待会话时
// 直投原文不经 LLM；零个/多个等待会话或续轮语境不进捷径。
func TestHubSmartSendShortcut(t *testing.T) {
	t.Run("「继续」唯一 perm 会话直投", func(t *testing.T) {
		fx := newRouteFixture(t)
		setPermSession(t, fx, fx.refs[0])
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应进循环"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "继续")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending {
			t.Fatal("捷径直投应 Pending=false")
		}
		if *calls != 0 {
			t.Fatalf("捷径不应进循环, calls=%d", *calls)
		}
		// Send 被调：hub store 落一条发往该会话的用户消息（SendChatPrompt 链路）。
		msg, ok := lastUserMessage(t, fx, fx.refs[0].ChatID)
		if !ok {
			t.Fatal("捷径应经 Send 投递用户原文（hub store 无用户消息）")
		}
		if msg.Text != "继续" {
			t.Fatalf("投递正文 = %q, 期望继续", msg.Text)
		}
		// 回执含会话名。
		want := fmt.Sprintf(hub.CmdText("zh-CN", "shortcut_sent"), "继续", "甲")
		if len(view.Replies) != 1 || view.Replies[0].Text != want {
			t.Fatalf("捷径回执 = %+v, want %q", view.Replies, want)
		}
	})

	t.Run("「好的」同样命中捷径", func(t *testing.T) {
		fx := newRouteFixture(t)
		setPermSession(t, fx, fx.refs[1])
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{}, nil
		})
		view, err := fx.app.HubSmartSend("req", "好的")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending || *calls != 0 {
			t.Fatalf("捷径应直投: pending=%v calls=%d", view.Pending, *calls)
		}
	})

	t.Run("英文 continue 唯一 perm 会话直投（捷径先于逃生门）", func(t *testing.T) {
		fx := newRouteFixture(t)
		setPermSession(t, fx, fx.refs[2])
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应进循环"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "Continue")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending {
			t.Fatal("英文 continue 命中捷径应 Pending=false（不得被逃生门拦下）")
		}
		if *calls != 0 {
			t.Fatalf("捷径不应进循环, calls=%d", *calls)
		}
		msg, ok := lastUserMessage(t, fx, fx.refs[2].ChatID)
		if !ok || msg.Text != "Continue" {
			t.Fatalf("捷径应直投原文（hub store 用户消息）: %v %+v", ok, msg)
		}
		want := fmt.Sprintf(hub.CmdText("zh-CN", "shortcut_sent"), "Continue", "丙")
		if len(view.Replies) != 1 || view.Replies[0].Text != want {
			t.Fatalf("捷径回执 = %+v, want %q", view.Replies, want)
		}
	})

	t.Run("英文 continue 零 perm 会话仍回落逃生门", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应进循环"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "continue")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("零 perm 时英文 continue 应回落逃生门（Pending=true）")
		}
		if *calls != 0 {
			t.Fatalf("逃生门不应进循环, calls=%d", *calls)
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("两个等待会话不进捷径", func(t *testing.T) {
		fx := newRouteFixture(t)
		setPermSession(t, fx, fx.refs[0])
		setPermSession(t, fx, fx.refs[1])
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"请指定会话"}}, nil
		})
		view, err := fx.app.HubSmartSend("req", "继续")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending || len(view.Replies) != 1 {
			t.Fatalf("多等待会话应进循环: %+v", view)
		}
		if *calls != 1 {
			t.Fatalf("应进循环, calls=%d", *calls)
		}
		if _, ok := lastUserMessage(t, fx, fx.refs[0].ChatID); ok {
			t.Fatal("多等待会话时捷径不得直投")
		}
	})

	t.Run("零个等待会话不进捷径", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"没有等待中的会话"}}, nil
		})
		if _, err := fx.app.HubSmartSend("req", "继续"); err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if *calls != 1 {
			t.Fatalf("零等待会话应进循环, calls=%d", *calls)
		}
	})

	t.Run("AskPending 语境下「继续」走续轮不走捷径", func(t *testing.T) {
		fx := newRouteFixture(t)
		setPermSession(t, fx, fx.refs[0]) // 有唯一等待会话也不得劫持续轮
		injectHubRunner(fx, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"确认吗？"}, AskPending: true}, nil
		})
		if _, err := fx.app.HubSmartSend("req", "继续修吗"); err != nil {
			t.Fatalf("ask HubSmartSend: %v", err)
		}
		var gotOpts agentapi.RunOpts
		fx.app.mu.Lock()
		fx.app.hubRunner = func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			gotOpts = opts
			return agentapi.RunResult{Replies: []string{"收到"}}, nil
		}
		fx.app.mu.Unlock()
		if _, err := fx.app.HubSmartSend("req", "继续"); err != nil {
			t.Fatalf("续轮 HubSmartSend: %v", err)
		}
		if !gotOpts.Continuation {
			t.Fatal("AskPending 语境下「继续」应以 Continuation 进循环")
		}
		if _, ok := lastUserMessage(t, fx, fx.refs[0].ChatID); ok {
			t.Fatal("续轮语境下捷径不得直投")
		}
	})
}

// TestHubSmartSendProgressAndCancel 覆盖「思考态」后端契约：
//   - 循环前推 route 阶段、每次工具调用推 tool 阶段（payload 带 tool/round）；
//   - 回调型循环桩（模拟真实模型往返）在途中可被 CancelHubSmartSend 打断，
//     该轮返回 Cancelled 且不投递任何消息。
func TestHubSmartSendProgressAndCancel(t *testing.T) {
	t.Run("阶段事件按序推送且 payload 携带工具信息", func(t *testing.T) {
		fx := newRouteFixture(t)
		var events []hubProgress
		fx.app.mu.Lock()
		fx.app.opts.Emit = func(name string, data ...any) {
			if name != "hub:progress" || len(data) == 0 {
				return
			}
			if p, ok := data[0].(hubProgress); ok && p.ID == "r1" {
				events = append(events, p)
			}
		}
		fx.app.mu.Unlock()
		injectHubRunner(fx, func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			if opts.Progress != nil {
				opts.Progress(agentapi.LoopProgress{Tool: "list_sessions", Round: 1})
				opts.Progress(agentapi.LoopProgress{Tool: "send", Round: 2})
			}
			return agentapi.RunResult{Replies: []string{"已发送"}, ToolSteps: 2}, nil
		})

		if _, err := fx.app.HubSmartSend("r1", "发给甲"); err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("阶段事件 = %+v, 期望 3 条", events)
		}
		if events[0].Step != hubStepRoute {
			t.Fatalf("首事件 = %+v, 期望 route", events[0])
		}
		for i, want := range []struct{ tool string; round int }{{"list_sessions", 1}, {"send", 2}} {
			p := events[i+1]
			if p.Step != hubStepTool || p.Tool != want.tool || p.Round != want.round {
				t.Fatalf("tool 事件[%d] = %+v, 期望 tool=%s round=%d", i, p, want.tool, want.round)
			}
		}
	})

	t.Run("在途取消返回 Cancelled 且不投递", func(t *testing.T) {
		fx := newRouteFixture(t)
		inRoute := make(chan struct{})
		fx.app.mu.Lock()
		fx.app.hubRunner = func(ctx context.Context, _ string, _ agentapi.RunOpts) (agentapi.RunResult, error) {
			close(inRoute)
			// 等取消传播：真实模型请求同样以 ctx 结束
			<-ctx.Done()
			return agentapi.RunResult{}, ctx.Err()
		}
		fx.app.mu.Unlock()

		done := make(chan HubPickView, 1)
		go func() {
			view, _ := fx.app.HubSmartSend("r3", "先别发")
			done <- view
		}()
		<-inRoute
		fx.app.CancelHubSmartSend("r3")

		view := <-done
		if !view.Cancelled {
			t.Fatalf("取消后应返回 Cancelled, got %+v", view)
		}
		if view.Pending {
			t.Fatal("取消不应回落待选（Pending 必须为 false）")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("取消后不得投递: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("取消不存在的请求静默忽略", func(t *testing.T) {
		fx := newRouteFixture(t)
		fx.app.CancelHubSmartSend("")
		fx.app.CancelHubSmartSend("never-started")
	})
}

// TestHubSmartSendBusyGuard 覆盖管理者循环入口的在途守卫：循环调用期间并发
// 提交的第二次调用直接返回 Pending=true，不调循环、不清续轮标记（防重复投递）。
func TestHubSmartSendBusyGuard(t *testing.T) {
	fx := newRouteFixture(t)

	// 循环桩：首次调用阻塞直到放行；若守卫失效第二次也进来，计数后立即返回。
	started := make(chan struct{})
	release := make(chan struct{})
	fx.app.mu.Lock()
	fx.app.hubRunner = func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
		select {
		case <-started:
			// 已有第一个调用在途：第二个并发调用不应到达这里，但若到达（守卫
			// 失效）也立即返回不阻塞，测试用后续断言失败。
			return agentapi.RunResult{Replies: []string{"不应到这"}}, nil
		default:
		}
		close(started)
		<-release
		return agentapi.RunResult{Replies: []string{"第一次问"}, AskPending: true}, nil
	}
	fx.app.mu.Unlock()

	first := make(chan HubPickView, 1)
	go func() {
		view, _ := fx.app.HubSmartSend("req", "第一次")
		first <- view
	}()

	// 等第一个调用真正进入循环（started 关闭即进入）。
	<-started

	// 并发第二次提交：应被在途守卫拦下，Pending=true。
	second, err := fx.app.HubSmartSend("req", "第二次")
	if err != nil {
		t.Fatalf("第二次 HubSmartSend: %v", err)
	}
	if !second.Pending {
		t.Fatal("在途期间第二次提交应 Pending=true")
	}

	// 续轮标记不被后到调用清零：第一个调用是 AskPending，完成后应为 true。
	close(release)
	view := <-first
	if view.Pending {
		t.Fatal("第一次调用应正常完成 Pending=false")
	}
	if !hubAskPendingOf(fx.app) {
		t.Fatal("AskPending 结果应记录（busy 期间不得清零）")
	}
}
