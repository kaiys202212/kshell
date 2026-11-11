package desktop

import (
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
		if len(replies) != 1 || !strings.Contains(replies[0].Text, "已发送") {
			t.Fatalf("群发回执 = %+v", replies)
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
		if len(replies) != 1 || !strings.Contains(replies[0].Text, "已发送") {
			t.Fatalf("单发回执 = %+v", replies)
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
}
