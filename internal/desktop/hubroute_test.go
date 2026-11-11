package desktop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
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
// executeRoute 的 ListSessions 编号映射）+ 真 hub.Console + fake Executor。
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
// 其余 idle），编号保持 ListSessions 顺序不重排。
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

// TestExecuteRouteDecision 覆盖 executeRoute 的五类动作与降级边界。
func TestExecuteRouteDecision(t *testing.T) {
	t.Run("group 群发按目标改写且进聚合", func(t *testing.T) {
		fx := newRouteFixture(t)
		d := hubtools.RouteDecision{
			Action:        "group",
			Targets:       []int{1, 3},
			Text:          "统一正文",
			PerTargetText: map[string]string{"1": "改写甲"},
		}
		replies, handled := executeRoute(fx.app, d, "用户原文")
		if !handled {
			t.Fatal("group 应 handled=true")
		}
		if len(fx.ex.sendCalls) != 2 {
			t.Fatalf("sendCalls = %+v", fx.ex.sendCalls)
		}
		if fx.ex.sendCalls[0] != [2]string{fx.refs[0].ChatID, "改写甲"} {
			t.Fatalf("目标1应收到改写文本: %+v", fx.ex.sendCalls[0])
		}
		// PerTargetText 未命中目标3：退 d.Text。
		if fx.ex.sendCalls[1] != [2]string{fx.refs[2].ChatID, "统一正文"} {
			t.Fatalf("目标3应回退统一正文: %+v", fx.ex.sendCalls[1])
		}
		wantPrefix := fmt.Sprintf(hub.CmdText("zh-CN", "group_sent"), "")
		if len(replies) != 1 || !strings.HasPrefix(replies[0].Text, wantPrefix) {
			t.Fatalf("群发回执 = %+v, want 前缀 %q", replies, wantPrefix)
		}
		if p := fx.c.MirrorPolicy(); !p.Active() || p.Count() != 2 {
			t.Fatalf("群发两目标应进聚合: active=%v count=%d", p.Active(), p.Count())
		}
	})

	t.Run("group 目标编号不存在则降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "group", Targets: []int{1, 9}, Text: "原文",
		}, "用户原文")
		if handled {
			t.Fatal("含不存在编号应 handled=false")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("single 指定编号直投该会话", func(t *testing.T) {
		fx := newRouteFixture(t)
		replies, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "single", Targets: []int{2}, Text: "单发正文",
		}, "用户原文")
		if !handled {
			t.Fatal("single 指定编号应 handled=true")
		}
		if len(fx.ex.sendCalls) != 1 || fx.ex.sendCalls[0] != [2]string{fx.refs[1].ChatID, "单发正文"} {
			t.Fatalf("sendCalls = %+v", fx.ex.sendCalls)
		}
		wantPrefix := fmt.Sprintf(hub.CmdText("zh-CN", "sent_to"), "")
		if len(replies) != 1 || !strings.HasPrefix(replies[0].Text, wantPrefix) {
			t.Fatalf("单发回执 = %+v, want 前缀 %q", replies, wantPrefix)
		}
	})

	t.Run("single 空 targets 交给调用方降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "single", Text: "原文",
		}, "用户原文")
		if handled {
			t.Fatal("single 空 targets 应 handled=false（调用方回落 current）")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("status 合成 #st 并透传回执", func(t *testing.T) {
		fx := newRouteFixture(t)
		fx.ex.statuses = []hub.SessionStatus{
			{Ref: fx.refs[0], State: "running", Summary: "状态摘要甲"},
			{Ref: fx.refs[1], State: "idle", Summary: "状态摘要乙"},
		}
		replies, handled := executeRoute(fx.app, hubtools.RouteDecision{Action: "status"}, "用户原文")
		if !handled {
			t.Fatal("status 应 handled=true")
		}
		if len(replies) != 1 {
			t.Fatalf("status 回执 = %+v", replies)
		}
		wantHead := fmt.Sprintf(hub.CmdText("zh-CN", "status_running"), 1)
		if !strings.Contains(replies[0].Text, wantHead) || !strings.Contains(replies[0].Text, "状态摘要甲") {
			t.Fatalf("#st 回执未透传: %q", replies[0].Text)
		}
	})

	t.Run("expand 合成 #gx 使用默认条数", func(t *testing.T) {
		fx := newRouteFixture(t)
		fx.ex.recent = map[string][]hub.Message{
			fx.refs[1].ChatID: {{Kind: "agent", Text: "展开详情乙"}},
		}
		// #gx 依赖 Console 最近列表编号语境，先执行一次 #l（fake 列表与
		// executeRoute 的编号映射同源）。
		fx.c.Handle("#l")
		replies, handled := executeRoute(fx.app, hubtools.RouteDecision{Action: "expand", ExpandNum: 2}, "用户原文")
		if !handled {
			t.Fatal("expand 应 handled=true")
		}
		if fx.ex.lastN != 5 {
			t.Fatalf("默认条数应为 5, got %d", fx.ex.lastN)
		}
		if len(replies) != 1 || !strings.Contains(replies[0].Text, "展开详情乙") {
			t.Fatalf("expand 回执未透传: %+v", replies)
		}
	})

	t.Run("expand 带条数走 #gx N count", func(t *testing.T) {
		fx := newRouteFixture(t)
		fx.ex.recent = map[string][]hub.Message{
			fx.refs[1].ChatID: {{Kind: "agent", Text: "展开详情乙"}},
		}
		fx.c.Handle("#l")
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "expand", ExpandNum: 2, ExpandCount: 7,
		}, "用户原文")
		if !handled {
			t.Fatal("expand 应 handled=true")
		}
		if fx.ex.lastN != 7 {
			t.Fatalf("条数应透传 7, got %d", fx.ex.lastN)
		}
	})

	t.Run("ask 返回前缀加问题", func(t *testing.T) {
		fx := newRouteFixture(t)
		replies, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "ask", Question: "你指的是哪个会话？",
		}, "用户原文")
		if !handled {
			t.Fatal("ask 应 handled=true")
		}
		want := hub.CmdText("zh-CN", "route_ask_prefix") + "你指的是哪个会话？"
		if len(replies) != 1 || replies[0].Text != want {
			t.Fatalf("ask 回执 = %+v, want %q", replies, want)
		}
	})

	t.Run("group 任一目标空文本整批降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		// 目标 2 无 PerTargetText 且通用 Text 为空：发送前判定，整批降级，
		// 不向目标 1 投递、不做部分投递。
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action:        "group",
			Targets:       []int{1, 2},
			Text:          "",
			PerTargetText: map[string]string{"1": "改写甲"},
		}, "用户原文")
		if handled {
			t.Fatal("group 空文本应 handled=false（整批降级）")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得部分投递: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("single 空白正文降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "single", Targets: []int{2}, Text: "   ",
		}, "用户原文")
		if handled {
			t.Fatal("single 空白正文应 handled=false")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("未知 action 降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "broadcast", Text: "原文",
		}, "用户原文")
		if handled {
			t.Fatal("未知 action 应 handled=false")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("expand 编号非法降级", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{
			Action: "expand", ExpandNum: 0,
		}, "用户原文")
		if handled {
			t.Fatal("expand ExpandNum<=0 应 handled=false")
		}
	})

	t.Run("status 无会话也 handled", func(t *testing.T) {
		fx := newRouteFixture(t)
		_, handled := executeRoute(fx.app, hubtools.RouteDecision{Action: "status"}, "用户原文")
		if !handled {
			t.Fatal("status 无会话应 handled=true（#st 自行渲染空态）")
		}
	})
}

// smartRoundOf 读取反问轮次（mu 保护）。
func smartRoundOf(a *App) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.smartAskRound
}

// injectSmartRouter 注入路由桩并返回调用计数指针。
func injectSmartRouter(fx *routeFixture, d hubtools.RouteDecision, handled bool) *int {
	calls := 0
	fx.app.mu.Lock()
	fx.app.smartRouter = func(context.Context, string) (hubtools.RouteDecision, bool) {
		calls++
		return d, handled
	}
	fx.app.mu.Unlock()
	return &calls
}

// TestHubSmartSend 覆盖桌面 Hub 智能路由入口：决策执行/反问轮次/降级回落。
func TestHubSmartSend(t *testing.T) {
	t.Run("group 决策执行并返回回执", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectSmartRouter(fx, hubtools.RouteDecision{
			Action: "group", Targets: []int{1, 3}, Text: "统一正文",
		}, true)
		view, err := fx.app.HubSmartSend("发给大家")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if view.Pending {
			t.Fatal("成功路由应 Pending=false")
		}
		if *calls != 1 {
			t.Fatalf("router 调用 = %d, 期望 1", *calls)
		}
		if len(fx.ex.sendCalls) != 2 {
			t.Fatalf("sendCalls = %+v", fx.ex.sendCalls)
		}
		wantPrefix := fmt.Sprintf(hub.CmdText("zh-CN", "group_sent"), "")
		if len(view.Replies) != 1 || !strings.HasPrefix(view.Replies[0].Text, wantPrefix) {
			t.Fatalf("回执 = %+v, want 前缀 %q", view.Replies, wantPrefix)
		}
		if got := smartRoundOf(fx.app); got != 0 {
			t.Fatalf("非 ask 决策后轮次应清零, got %d", got)
		}
	})

	t.Run("ask 反问计入轮次，第三次超限回落待选", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectSmartRouter(fx, hubtools.RouteDecision{
			Action: "ask", Question: "你指的是哪个会话？",
		}, true)
		want := hub.CmdText("zh-CN", "route_ask_prefix") + "你指的是哪个会话？"

		// 第 1、2 次：仍路由，返回单条反问。
		for i := 1; i <= 2; i++ {
			view, err := fx.app.HubSmartSend("这句给谁")
			if err != nil {
				t.Fatalf("第 %d 次 HubSmartSend: %v", i, err)
			}
			if view.Pending || len(view.Replies) != 1 || view.Replies[0].Text != want {
				t.Fatalf("第 %d 次回执 = %+v pending=%v", i, view.Replies, view.Pending)
			}
			if got := smartRoundOf(fx.app); got != i {
				t.Fatalf("第 %d 次后轮次 = %d, 期望 %d", i, got, i)
			}
		}

		// 第 3 次：轮次已满，不再调 router，Pending=true 且轮次清零。
		view, err := fx.app.HubSmartSend("第三次")
		if err != nil {
			t.Fatalf("第三次 HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("轮次超限应 Pending=true")
		}
		if *calls != 2 {
			t.Fatalf("轮次超限不应调 router, calls=%d", *calls)
		}
		if got := smartRoundOf(fx.app); got != 0 {
			t.Fatalf("轮次超限后应清零, got %d", got)
		}
	})

	t.Run("router 不消费则回落待选", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectSmartRouter(fx, hubtools.RouteDecision{}, false)
		view, err := fx.app.HubSmartSend("原文不能丢")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("handled=false 应 Pending=true 交前端回落待选")
		}
		if *calls != 1 {
			t.Fatalf("router 调用 = %d, 期望 1", *calls)
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("模型未配（默认 router）回落待选", func(t *testing.T) {
		fx := newRouteFixture(t) // 未注入 smartRouter，走 weixinInputRouter
		view, err := fx.app.HubSmartSend("没配模型")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("模型未配应 Pending=true")
		}
		if len(fx.ex.sendCalls) != 0 {
			t.Fatalf("不得发送: %+v", fx.ex.sendCalls)
		}
	})

	t.Run("逃生门命令不进路由", func(t *testing.T) {
		fx := newRouteFixture(t)
		calls := injectSmartRouter(fx, hubtools.RouteDecision{Action: "group", Targets: []int{1, 2}, Text: "x"}, true)
		view, err := fx.app.HubSmartSend("#g 1 hi")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("逃生门应 Pending=true 交前端走命令路径")
		}
		if *calls != 0 {
			t.Fatalf("逃生门不应调 router, calls=%d", *calls)
		}
	})

	t.Run("待选菜单挂起时让路", func(t *testing.T) {
		fx := newRouteFixture(t)
		// 先建立待选菜单（Busy=true）。
		if pick, err := fx.app.HubPickTarget("先选目标"); err != nil || !pick.Pending {
			t.Fatalf("fixture 应能建立待选菜单: view=%+v err=%v", pick, err)
		}
		calls := injectSmartRouter(fx, hubtools.RouteDecision{Action: "status"}, true)
		view, err := fx.app.HubSmartSend("菜单挂起中的文本")
		if err != nil {
			t.Fatalf("HubSmartSend: %v", err)
		}
		if !view.Pending {
			t.Fatal("Busy 应 Pending=true")
		}
		if *calls != 0 {
			t.Fatalf("Busy 不应调 router, calls=%d", *calls)
		}
	})

	t.Run("非 ask 决策打断反问链并清零轮次", func(t *testing.T) {
		fx := newRouteFixture(t)

		// 第一次 ask → 轮次 1。
		injectSmartRouter(fx, hubtools.RouteDecision{Action: "ask", Question: "问一次"}, true)
		if _, err := fx.app.HubSmartSend("第一次"); err != nil {
			t.Fatalf("ask: %v", err)
		}
		if got := smartRoundOf(fx.app); got != 1 {
			t.Fatalf("ask 后轮次 = %d, 期望 1", got)
		}

		// 换成 status 决策 → 轮次清零。
		fx.app.mu.Lock()
		fx.app.smartRouter = func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "expand", ExpandNum: 1}, true
		}
		fx.app.mu.Unlock()
		fx.ex.recent = map[string][]hub.Message{
			fx.refs[0].ChatID: {{Kind: "agent", Text: "详情甲"}},
		}
		fx.c.Handle("#l")
		if _, err := fx.app.HubSmartSend("看看详情"); err != nil {
			t.Fatalf("expand: %v", err)
		}
		if got := smartRoundOf(fx.app); got != 0 {
			t.Fatalf("非 ask 决策后轮次 = %d, 期望 0", got)
		}

		// 再来一次 ask：应重新从 1 计数（说明上一轮确实清零）。
		fx.app.mu.Lock()
		fx.app.smartRouter = func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "ask", Question: "再问一次"}, true
		}
		fx.app.mu.Unlock()
		if _, err := fx.app.HubSmartSend("第二次问"); err != nil {
			t.Fatalf("ask 2: %v", err)
		}
		if got := smartRoundOf(fx.app); got != 1 {
			t.Fatalf("重新 ask 后轮次 = %d, 期望 1", got)
		}
	})
}
