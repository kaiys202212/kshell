package weixin

import (
	"context"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// routerStub 记录路由调用次数/入参，并按 fn 回放决策（nil fn 表示不消费）。
type routerStub struct {
	calls  int
	inputs []string
	fn     func(ctx context.Context, input string) (hubtools.RouteDecision, bool)
}

// router 返回可注入 bridge 的桩闭包。
func (s *routerStub) router() InputRouter {
	return func(ctx context.Context, input string) (hubtools.RouteDecision, bool) {
		s.calls++
		s.inputs = append(s.inputs, input)
		if s.fn == nil {
			return hubtools.RouteDecision{}, false
		}
		return s.fn(ctx, input)
	}
}

// newRouteBridge 构造指向测试服务端的桥（封装既有 newBridge 套路）。
func newRouteBridge(t *testing.T, ex hub.Executor) (*Bridge, *recorder) {
	t.Helper()
	rc := &recorder{}
	return newBridge(t, "u1", ex, rc.handler(t)), rc
}

// TestBridgeRouterHook 覆盖微信入站智能路由接线：逃生门/引用/菜单挂起让路，
// 决策落 Console，反问轮次纪律与不可执行降级。
func TestBridgeRouterHook(t *testing.T) {
	t.Run("逃生门不进路由且直发路径照旧", func(t *testing.T) {
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{}
		b.SetRouter(stub.router())

		for _, in := range []string{"#st", "@1 hi", "continue"} {
			b.HandleInbound("u1", "ct", in)
		}
		if stub.calls != 0 {
			t.Fatalf("逃生门输入不应进路由, 实际调用 %d 次 (%v)", stub.calls, stub.inputs)
		}
		// @1 hi / Continue 走既有直发当前会话路径（#st 只回状态不投递）。
		if len(ex.sentTexts) != 2 || ex.sentTexts[0] != "@1 hi" || ex.sentTexts[1] != "continue" {
			t.Fatalf("直发路径异常: %v", ex.sentTexts)
		}
	})

	t.Run("引用消息不进路由", func(t *testing.T) {
		ex := &fakeExec{
			sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}, {ChatID: "c2", Title: "会话B"}},
			hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"},
		}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{}
		b.SetRouter(stub.router())
		b.rememberSvrID("9001", "c2")

		b.HandleInboundMsg(context.Background(), quoteMsg("ct-1", "9001", "【会话B】旧回复", "续问"))
		if stub.calls != 0 {
			t.Fatalf("引用消息不应进路由, 实际调用 %d 次", stub.calls)
		}
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "续问" {
			t.Fatalf("引用路径应直投被引用会话: %v", ex.sentTexts)
		}
	})

	t.Run("group 决策落 Console 群发并回执出站", func(t *testing.T) {
		ex := &fakeExec{
			sessions: []hub.SessionRef{{ChatID: "c1", Title: "甲"}, {ChatID: "c2", Title: "乙"}},
			hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "甲"},
		}
		b, rc := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "group", Targets: []int{1, 2}, Text: "同步一下"}, true
		}}
		b.SetRouter(stub.router())

		b.HandleInbound("u1", "ct-1", "两边都同步")
		if stub.calls != 1 {
			t.Fatalf("路由调用次数 = %d, 期望 1", stub.calls)
		}
		if len(ex.sentTexts) != 2 || ex.sentTexts[0] != "同步一下" || ex.sentTexts[1] != "同步一下" {
			t.Fatalf("group 决策应群发两个目标: %v", ex.sentTexts)
		}
		// 群发回执经 deliver 出站（一条 sendmessage）。
		if n := len(rc.sends()); n != 1 {
			t.Fatalf("群发回执 sendmessage 次数 = %d, 期望 1", n)
		}
		if b.routeRoundValue() != 0 {
			t.Errorf("非 ask 决策应清零轮次, 实际 %d", b.routeRoundValue())
		}
	})

	t.Run("ask 反问轮次与第三轮降级", func(t *testing.T) {
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, rc := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "ask", Question: "要发给哪个会话？"}, true
		}}
		b.SetRouter(stub.router())

		// 第一轮：反问出站，轮次 1。
		b.HandleInbound("u1", "ct-1", "帮我看看")
		if stub.calls != 1 || b.routeRoundValue() != 1 {
			t.Fatalf("第一轮 calls=%d round=%d, 期望 1/1", stub.calls, b.routeRoundValue())
		}
		want := hub.CmdText("zh-CN", "route_ask_prefix") + "要发给哪个会话？"
		if got := sendText(t, rc.sends()[0].body); got != want {
			t.Fatalf("反问文本 = %q, 期望 %q", got, want)
		}

		// 第二轮：自由文本再进路由（轮次 2）。
		b.HandleInbound("u1", "ct-2", "第一个")
		if stub.calls != 2 || b.routeRoundValue() != 2 {
			t.Fatalf("第二轮 calls=%d round=%d, 期望 2/2", stub.calls, b.routeRoundValue())
		}

		// 第三轮：不再进路由，原文走 console.Handle 降级，轮次清零。
		b.HandleInbound("u1", "ct-3", "就这个")
		if stub.calls != 2 {
			t.Fatalf("第三轮不应进路由, calls=%d", stub.calls)
		}
		if b.routeRoundValue() != 0 {
			t.Fatalf("第三轮降级后轮次应清零, 实际 %d", b.routeRoundValue())
		}
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "就这个" {
			t.Fatalf("第三轮应把原文发当前会话: %v", ex.sentTexts)
		}
	})

	t.Run("handled=false 回落原文", func(t *testing.T) {
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{}, false
		}}
		b.SetRouter(stub.router())

		b.HandleInbound("u1", "ct-1", "普通消息")
		if stub.calls != 1 {
			t.Fatalf("路由调用次数 = %d, 期望 1", stub.calls)
		}
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "普通消息" {
			t.Fatalf("handled=false 应回落 Handle(原文): %v", ex.sentTexts)
		}
	})

	t.Run("菜单挂起让路", func(t *testing.T) {
		ex := &fakeExec{
			sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}},
			hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"},
		}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "status"}, true
		}}
		b.SetRouter(stub.router())

		if _, pending := b.PickTarget("暂存正文"); !pending {
			t.Fatal("应成功建立待选菜单")
		}
		if !b.console.Busy() {
			t.Fatal("待选菜单存在时 console 应为 busy")
		}
		b.HandleInbound("u1", "ct-1", "自由文本")
		if stub.calls != 0 {
			t.Fatalf("菜单挂起时不应进路由, 实际调用 %d 次", stub.calls)
		}
	})

	t.Run("自定义 directPatterns 生效", func(t *testing.T) {
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "status"}, true
		}}
		b.SetRouter(stub.router())
		b.SetDirectPatterns(func() []string { return []string{"hello"} })

		b.HandleInbound("u1", "ct-1", "hello 世界")
		if stub.calls != 0 {
			t.Fatalf("自定义逃生门命中不应进路由, 实际调用 %d 次", stub.calls)
		}
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "hello 世界" {
			t.Fatalf("逃生门输入应直发当前会话: %v", ex.sentTexts)
		}
	})

	t.Run("空 directPatterns 回落默认逃生门", func(t *testing.T) {
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "status"}, true
		}}
		b.SetRouter(stub.router())
		b.SetDirectPatterns(func() []string { return nil })

		// explain 已移出默认集（进管理者循环）；continue 仍是默认逃生门词。
		b.HandleInbound("u1", "ct-1", "continue 一下")
		if stub.calls != 0 {
			t.Fatalf("默认逃生门 continue 不应进路由, 实际调用 %d 次", stub.calls)
		}
		b.HandleInbound("u1", "ct-1", "explain 一下")
		if stub.calls != 1 {
			t.Fatalf("默认集已无 explain, 应进路由, 实际调用 %d 次", stub.calls)
		}
	})

	t.Run("命令不进路由而纯数字进路由", func(t *testing.T) {
		ex := &fakeExec{
			sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}, {ChatID: "c2", Title: "会话B"}},
			hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"},
		}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{}, false // 不消费：回落 Handle 原文
		}}
		b.SetRouter(stub.router())

		b.HandleInbound("u1", "ct-0", "#l") // 命令：无条件逃生门，不问路由器
		if stub.calls != 0 {
			t.Fatalf("命令不应进路由, 实际调用 %d 次 (%v)", stub.calls, stub.inputs)
		}
		b.HandleInbound("u1", "ct-1", "2") // 纯数字：按 T2 规格进路由（语境由路由器决断）
		if stub.calls != 1 {
			t.Fatalf("纯数字应进路由, 实际调用 %d 次", stub.calls)
		}
		// 路由器不消费：回落 Handle 原文，仍走既有编号切换语义（#l 后 2 → c2）。
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c2" {
			t.Fatalf("纯数字应回落编号切换: %v", ex.switchCalls)
		}
		// 全角数字同样进路由（与半角口径一致，语境判断交给路由器）。
		b.HandleInbound("u1", "ct-2", "２")
		if stub.calls != 2 {
			t.Fatalf("全角数字应进路由, 实际调用 %d 次", stub.calls)
		}
	})
}

// TestBridgeExecuteRouteText 覆盖 bridge 侧决策落 Console 的边界：编号缺失/
// 空文本降级，status/expand 合成命令，ask 前缀取自 bridge 语言。
func TestBridgeExecuteRouteText(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "c1", Title: "甲"}, {ChatID: "c2", Title: "乙"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "甲"},
	}
	b, _ := newRouteBridge(t, ex)

	t.Run("group 编号越界返回 nil", func(t *testing.T) {
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "group", Targets: []int{1, 9}, Text: "x",
		})
		if replies != nil {
			t.Fatalf("越界编号应降级 nil, got %v", replies)
		}
	})

	t.Run("group 空文本整批降级", func(t *testing.T) {
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "group", Targets: []int{1, 2}, PerTargetText: map[string]string{"1": "甲专属"},
		})
		if replies != nil {
			t.Fatalf("任一目标空文本应整批降级 nil, got %v", replies)
		}
	})

	t.Run("single 空 targets 返回 nil", func(t *testing.T) {
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "single", Text: "x",
		})
		if replies != nil {
			t.Fatalf("single 空 targets 应降级 nil, got %v", replies)
		}
	})

	t.Run("ask 文案取 bridge 语言前缀", func(t *testing.T) {
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "ask", Question: "哪个？",
		})
		if len(replies) != 1 || !strings.HasPrefix(replies[0].Text, hub.CmdText("zh-CN", "route_ask_prefix")) {
			t.Fatalf("ask 回复缺语言前缀: %v", replies)
		}
	})

	t.Run("expand 合成 #gx 命令", func(t *testing.T) {
		// 先建立编号映射（#gx 依赖 lastList）。
		b.console.Handle("#l")
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "expand", ExpandNum: 1, ExpandCount: 3,
		})
		if len(replies) == 0 {
			t.Fatal("expand 应返回命令回复（无消息时也回提示）")
		}
		// #gx 依赖 #l 建立的编号语境；语境丢失会回 no_mapping（越界文案）。
		// 断言回复不含该文案，确保合成命令确实命中会话 1 而非降级。
		for _, r := range replies {
			if strings.Contains(r.Text, hub.CmdText("zh-CN", "no_mapping")) {
				t.Fatalf("expand 不应触发 no_mapping: %v", replies)
			}
		}
	})

	t.Run("status 合成 #st 透传状态行", func(t *testing.T) {
		ex.statuses = []hub.SessionStatus{{Ref: ex.sessions[0], State: "running", Summary: "正在修复登录"}}
		defer func() { ex.statuses = nil }()
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{Action: "status"})
		if len(replies) == 0 {
			t.Fatal("status 应返回 #st 回复")
		}
		if !strings.Contains(replies[0].Text, "正在修复登录") {
			t.Fatalf("status 回复缺状态摘要: %q", replies[0].Text)
		}
	})

	t.Run("single 合法编号直投该会话", func(t *testing.T) {
		b.console.Handle("#l") // 建立编号映射语境
		before := len(ex.sentTexts)
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action: "single", Targets: []int{2}, Text: "单发乙",
		})
		if len(replies) == 0 {
			t.Fatal("single 应返回回执")
		}
		if len(ex.sentTexts) != before+1 || ex.sentTexts[before] != "单发乙" {
			t.Fatalf("single 应回执目标会话乙: %v", ex.sentTexts)
		}
	})

	t.Run("group 优先 PerTargetText", func(t *testing.T) {
		before := len(ex.sentTexts)
		replies := b.executeRouteText(context.Background(), "原文", hubtools.RouteDecision{
			Action:        "group",
			Targets:       []int{1, 2},
			Text:          "统一正文",
			PerTargetText: map[string]string{"1": "甲专属"},
		})
		if len(replies) == 0 {
			t.Fatal("group 应返回回执")
		}
		if len(ex.sentTexts) != before+2 || ex.sentTexts[before] != "甲专属" || ex.sentTexts[before+1] != "统一正文" {
			t.Fatalf("目标1应收到改写文本、目标2退统一正文: %v", ex.sentTexts[before:])
		}
	})
}

// TestBridgeRouteAttachmentBypass 回归：带附件合并的文本一律走确定性 Handle
// 原路径，不进智能路由（附件说明行与用户文字按原样转交当前会话，队列语义照旧）。
func TestBridgeRouteAttachmentBypass(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}, {ChatID: "c2", Title: "会话B"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"},
	}
	b, _ := newRouteBridge(t, ex)
	stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
		return hubtools.RouteDecision{Action: "group", Targets: []int{1, 2}, Text: "路由正文"}, true
	}}
	b.SetRouter(stub.router())

	// 附件入队（直接注入，绕过下载路径）。
	b.mu.Lock()
	b.queue = append(b.queue, queuedFile{Name: "需求.pdf", Path: t.TempDir() + "/需求.pdf"})
	b.mu.Unlock()

	b.HandleInbound("u1", "ct-1", "帮我修复登录")
	if stub.calls != 0 {
		t.Fatalf("带附件文本不应进路由, 实际调用 %d 次", stub.calls)
	}
	if len(ex.sentTexts) != 1 {
		t.Fatalf("应走 Handle 原路径合并投递, 实际: %v", ex.sentTexts)
	}
	if !strings.Contains(ex.sentTexts[0], "[附件] 需求.pdf") || !strings.Contains(ex.sentTexts[0], "帮我修复登录") {
		t.Fatalf("合并文本缺附件说明行或用户文字: %q", ex.sentTexts[0])
	}
	if b.QueuedFiles() != 0 {
		t.Fatalf("合并成功后队列应清空, 实际 %d", b.QueuedFiles())
	}
}

// TestBridgeRouteRoundClearedByInterruption 回归：ask 反问之后插入逃生门/
// 命令/引用等不进路由的消息，routeRound 必须清零，轮次不得悬挂。
func TestBridgeRouteRoundClearedByInterruption(t *testing.T) {
	newB := func(t *testing.T) (*Bridge, *fakeExec) {
		t.Helper()
		ex := &fakeExec{
			sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}, {ChatID: "c2", Title: "会话B"}},
			hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"},
		}
		b, _ := newRouteBridge(t, ex)
		stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
			return hubtools.RouteDecision{Action: "ask", Question: "要发给哪个会话？"}, true
		}}
		b.SetRouter(stub.router())
		return b, ex
	}

	setup := func(b *Bridge) {
		b.HandleInbound("u1", "ct-a", "帮我看看") // 反问一轮
		if b.routeRoundValue() != 1 {
			t.Fatalf("前置反问轮次 = %d, 期望 1", b.routeRoundValue())
		}
	}

	t.Run("逃生门打断清零", func(t *testing.T) {
		b, ex := newB(t)
		setup(b)
		b.HandleInbound("u1", "ct-b", "continue") // 默认逃生门：直发，不进路由
		if b.routeRoundValue() != 0 {
			t.Fatalf("逃生门打断后轮次应清零, 实际 %d", b.routeRoundValue())
		}
		if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "continue" {
			t.Fatalf("逃生门应直发当前会话: %v", ex.sentTexts)
		}
	})

	t.Run("命令打断清零", func(t *testing.T) {
		b, _ := newB(t)
		setup(b)
		b.HandleInbound("u1", "ct-b", "#l")
		if b.routeRoundValue() != 0 {
			t.Fatalf("命令打断后轮次应清零, 实际 %d", b.routeRoundValue())
		}
	})

	t.Run("引用打断清零", func(t *testing.T) {
		b, _ := newB(t)
		setup(b)
		b.rememberSvrID("9001", "c2")
		b.HandleInboundMsg(context.Background(), quoteMsg("ct-b", "9001", "【会话B】旧回复", "续问"))
		if b.routeRoundValue() != 0 {
			t.Fatalf("引用打断后轮次应清零, 实际 %d", b.routeRoundValue())
		}
	})

	t.Run("菜单挂起打断清零", func(t *testing.T) {
		b, _ := newB(t)
		setup(b)
		if _, pending := b.PickTarget("暂存正文"); !pending {
			t.Fatal("应成功建立待选菜单")
		}
		b.HandleInbound("u1", "ct-b", "自由文本")
		if b.routeRoundValue() != 0 {
			t.Fatalf("菜单挂起打断后轮次应清零, 实际 %d", b.routeRoundValue())
		}
	})
}
