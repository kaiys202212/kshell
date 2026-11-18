package weixin

import (
	"context"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// TestBridgeRouteFailureFallsBackToPick 是 2026-11-18 真机事故的回归锚点：
// 智能链路（管理者循环 + 单发路由）全部失败时，微信入站自由文本**不得**
// 直投当前会话（事故里投给了 strategy 的 CodeBuddy，导致它跨项目自建会话），
// 必须改为一律进待选菜单，并附一条说明失败原因的回执。
func TestBridgeRouteFailureFallsBackToPick(t *testing.T) {
	newEx := func() *fakeExec {
		return &fakeExec{
			sessions: []hub.SessionRef{
				{ChatID: "c1", Title: "strategy · CodeBuddy", Workspace: `d:\ws\strategy`},
				{ChatID: "c2", Title: "kshell · Claude Code", Workspace: `D:\ws\kshell`},
			},
			hasCur:  true,
			current: hub.SessionRef{ChatID: "c1", Title: "strategy · CodeBuddy"},
		}
	}

	t.Run("管理者失败且路由不消费进待选而不直投", func(t *testing.T) {
		ex := newEx()
		b, rc := newRouteBridge(t, ex)
		// 管理者循环尝试过但失败（handled=false + FailureKey），单发路由同样不消费。
		b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
			return ManagerOutcome{FailureKey: "err.router.llm_not_ready"}, false
		})
		b.SetRouter((&routerStub{}).router())

		b.HandleInbound("u1", "ct-1", "在 kshell 项目中新建一个 claudecode 的会话")

		if len(ex.sentTexts) != 0 {
			t.Fatalf("路由失败不得直投当前会话，实际投递: %v", ex.sentTexts)
		}
		if !b.console.Busy() {
			t.Fatal("路由失败应建立待选菜单（暂存正文待用户选目标）")
		}
		sends := rc.sends()
		if len(sends) == 0 {
			t.Fatal("应至少回一条降级说明")
		}
		first := sendText(t, sends[0].body)
		if !strings.Contains(first, "未配置可用模型") {
			t.Fatalf("首条回执应说明失败原因（未配置可用模型），实际: %q", first)
		}
		// 菜单本身仍要能列出会话编号，用户才能继续。
		all := ""
		for _, s := range sends {
			all += sendText(t, s.body)
		}
		if !strings.Contains(all, "kshell · Claude Code") {
			t.Fatalf("待选菜单应列出可选会话，实际: %q", all)
		}
	})

	t.Run("回执按语言渲染英文", func(t *testing.T) {
		ex := newEx()
		b, rc := newRouteBridge(t, ex)
		b.lang = "en"
		b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
			return ManagerOutcome{FailureKey: "err.router.loop_failed"}, false
		})
		b.SetRouter((&routerStub{}).router())

		b.HandleInbound("u1", "ct-1", "fix the login bug across projects")
		sends := rc.sends()
		if len(sends) == 0 {
			t.Fatal("应有降级回执")
		}
		if got := sendText(t, sends[0].body); !strings.Contains(got, "Smart routing unavailable") {
			t.Fatalf("英文态回执应为英文说明，实际: %q", got)
		}
	})

	t.Run("失败原因缺省时用未识别目标", func(t *testing.T) {
		ex := newEx()
		b, rc := newRouteBridge(t, ex)
		// 管理者未装配、路由不消费：仍需进待选。
		b.SetRouter((&routerStub{}).router())

		b.HandleInbound("u1", "ct-1", "随便做点什么")
		if len(ex.sentTexts) != 0 {
			t.Fatalf("不得直投当前会话: %v", ex.sentTexts)
		}
		sends := rc.sends()
		if len(sends) == 0 {
			t.Fatal("应有降级回执")
		}
		if got := sendText(t, sends[0].body); !strings.Contains(got, "未识别到投递目标") {
			t.Fatalf("缺省原因应为「未识别到投递目标」，实际: %q", got)
		}
	})

	t.Run("未知失败 key 不透出原始 key", func(t *testing.T) {
		// 后端将来新增 key 但桥层忘了补文案时，用户看到的是兜底原因而不是
		// `err.router.some_new_key` 这种内部标识。
		ex := newEx()
		b, rc := newRouteBridge(t, ex)
		b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
			return ManagerOutcome{FailureKey: "err.router.brand_new_key"}, false
		})
		b.SetRouter((&routerStub{}).router())

		b.HandleInbound("u1", "ct-1", "随便做点什么")
		sends := rc.sends()
		if len(sends) == 0 {
			t.Fatal("应有降级回执")
		}
		got := sendText(t, sends[0].body)
		if strings.Contains(got, "err.router.") {
			t.Fatalf("不得把内部 key 透给用户: %q", got)
		}
		if !strings.Contains(got, "未识别到投递目标") {
			t.Fatalf("未知 key 应回落兜底原因，实际: %q", got)
		}
	})

	t.Run("无活跃会话时降级仍列出新建入口", func(t *testing.T) {
		// 没有任何会话时 listForPending 仍给出「新建」哨兵入口（pending=true），
		// 所以这里收到的是正常待选菜单——关键是原文不投给当前会话。
		ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
		b, rc := newRouteBridge(t, ex)
		b.SetRouter((&routerStub{}).router())

		b.HandleInbound("u1", "ct-1", "帮我看看")
		if len(ex.sentTexts) != 0 {
			t.Fatalf("无会话也不得直投: %v", ex.sentTexts)
		}
		if len(rc.sends()) == 0 {
			t.Fatal("应回执降级说明与菜单")
		}
		if got := sendText(t, rc.sends()[0].body); !strings.Contains(got, "未识别到投递目标") {
			t.Fatalf("回执应说明失败原因，实际: %q", got)
		}
	})
}

// TestBridgeRouteGateOffDirectPasses 回归保护：AgentHub 总开关关闭（门禁 false）时，
// 自由文本仍走命令层直投当前会话——这是"未启用智能路由"的正常用法，不得被拦。
func TestBridgeRouteGateOffDirectPasses(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
	b, _ := newRouteBridge(t, ex)
	router := &routerStub{}
	mgrCalled := 0
	b.SetRouter(router.router())
	b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
		mgrCalled++
		return ManagerOutcome{}, false
	})
	b.SetRouteGate(func() bool { return false })

	b.HandleInbound("u1", "ct-1", "发给当前会话就好")

	if mgrCalled != 0 || router.calls != 0 {
		t.Fatalf("门禁关闭不应调用智能链路: mgr=%d router=%d", mgrCalled, router.calls)
	}
	if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "发给当前会话就好" {
		t.Fatalf("门禁关闭应直投当前会话: %v", ex.sentTexts)
	}
	if b.console.Busy() {
		t.Fatal("门禁关闭不应建立待选菜单")
	}
}

// TestBridgeRouteGateDefaultAllows 回归：未装配门禁（nil）时按允许处理，
// 保持既有一切行为（既有测试与旧装配路径零影响）。
func TestBridgeRouteGateDefaultAllows(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "c1", Title: "甲"}, {ChatID: "c2", Title: "乙"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "c1", Title: "甲"},
	}
	b, _ := newRouteBridge(t, ex)
	stub := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
		return hubtools.RouteDecision{Action: "single", Targets: []int{2}, Text: "改写后"}, true
	}}
	b.SetRouter(stub.router())

	b.HandleInbound("u1", "ct-1", "发给乙")
	if stub.calls != 1 {
		t.Fatalf("未装配门禁应视为允许，路由调用 = %d", stub.calls)
	}
	if len(ex.sentTexts) != 1 || ex.sentTexts[0] != "改写后" {
		t.Fatalf("路由决策应正常执行: %v", ex.sentTexts)
	}
}

// TestBridgeRouteFailureTextsBilingual 新增文案 key 必须 en/zh 成对非空。
func TestBridgeRouteFailureTextsBilingual(t *testing.T) {
	for _, key := range []string{"route.pick_degraded", "route.pick_unavailable"} {
		p, ok := bridgeTexts[key]
		if !ok {
			t.Fatalf("缺少文案 key: %s", key)
		}
		if p.En == "" || p.Zh == "" {
			t.Fatalf("key %s 的 en/zh 之一为空: %+v", key, p)
		}
	}
}
