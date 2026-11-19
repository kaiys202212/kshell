package weixin

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// queueAttachment 直接注入一条待转交附件（绕过下载/解密路径）。
func queueAttachment(t *testing.T, b *Bridge, name string) {
	t.Helper()
	b.mu.Lock()
	b.queue = append(b.queue, queuedFile{Name: name, Path: t.TempDir() + "/" + name})
	b.mu.Unlock()
}

// 2026-11-19 真机回归：挂起反问期间用户「先发图、再发文字回答」，回答必须
// 显式续跑原任务（Resume 不受短输入门槛限制），不得因附件合并被当成普通文本
// 直投当前会话，也不得丢附件。
func TestBridgeMergedAttachmentAnswerResumesAsk(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "legacy", Title: "无关会话"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "legacy", Title: "无关会话"},
	}
	b, rc := newRouteBridge(t, ex)
	b.SetRouteGate(func() bool { return true })
	b.SetAskPending(func() bool { return true })

	var resumed []string
	b.SetAskResumer(func(_ context.Context, input string) (ManagerOutcome, bool) {
		resumed = append(resumed, input)
		return ManagerOutcome{Replies: []string{"已按问题1继续。"}}, true
	})
	router := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
		return hubtools.RouteDecision{Action: "group", Targets: []int{1}, Text: "不应发生"}, true
	}}
	b.SetRouter(router.router())

	queueAttachment(t, b, "image.jpg")
	b.HandleInbound("u1", "ct-2", "问题1给1，问题2从服务器上找日志")

	if len(resumed) != 1 {
		t.Fatalf("AskResumer 调用次数 = %d, 期望 1 (%v)", len(resumed), resumed)
	}
	if !strings.Contains(resumed[0], "[附件] image.jpg") || !strings.Contains(resumed[0], "问题1给1") {
		t.Fatalf("续跑输入应含附件说明行与用户答案: %q", resumed[0])
	}
	if router.calls != 0 {
		t.Fatalf("挂起续跑不应再走单发路由, 实际调用 %d 次", router.calls)
	}
	if len(ex.sentTexts) != 0 {
		t.Fatalf("合并文本不得直投当前会话: %v", ex.sentTexts)
	}
	if b.QueuedFiles() != 0 {
		t.Fatalf("续跑成功后队列应清空, 实际 %d", b.QueuedFiles())
	}
	// 附件已随续跑输入转交，不得再补一条「待转交」回执。
	for _, s := range rc.sends() {
		if strings.Contains(sendText(t, s.body), "已收到 image.jpg") {
			t.Fatalf("续跑成功后不应再发附件待转交回执: %q", sendText(t, s.body))
		}
	}
}

// 续跑失败（模型不可用等）：转待选并保留附件队列，绝不直投当前会话。
func TestBridgeMergedAttachmentAnswerResumeFailurePicks(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}, {ChatID: "c2", Title: "会话B"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "legacy", Title: "无关会话"},
	}
	b, _ := newRouteBridge(t, ex)
	b.SetRouteGate(func() bool { return true })
	b.SetAskPending(func() bool { return true })
	b.SetAskResumer(func(context.Context, string) (ManagerOutcome, bool) {
		return ManagerOutcome{FailureKey: "err.router.loop_failed"}, false
	})
	b.SetRouter((&routerStub{}).router()) // 不消费

	queueAttachment(t, b, "image.jpg")
	b.HandleInbound("u1", "ct-2", "问题1给1")

	if len(ex.sentTexts) != 0 {
		t.Fatalf("续跑失败不得直投当前会话: %v", ex.sentTexts)
	}
	if !b.console.Busy() {
		t.Fatal("续跑失败应转待选菜单")
	}
	if b.QueuedFiles() != 1 {
		t.Fatalf("待选阶段队列应保留, 实际 %d", b.QueuedFiles())
	}
}

// 智能链路启用、但当前没有挂起反问时，附件合并文本同样不得直投当前会话：
// 转待选让用户指定目标（修复 2）。
func TestBridgeMergedAttachmentWithoutAskPicksTarget(t *testing.T) {
	ex := &fakeExec{
		sessions: []hub.SessionRef{{ChatID: "c1", Title: "会话A"}},
		hasCur:   true, current: hub.SessionRef{ChatID: "legacy", Title: "无关会话"},
	}
	b, rc := newRouteBridge(t, ex)
	b.SetRouteGate(func() bool { return true })
	router := &routerStub{fn: func(context.Context, string) (hubtools.RouteDecision, bool) {
		return hubtools.RouteDecision{Action: "single", Targets: []int{1}, Text: "不应发生"}, true
	}}
	b.SetRouter(router.router())

	queueAttachment(t, b, "需求.pdf")
	b.HandleInbound("u1", "ct-1", "帮我修复登录")

	if router.calls != 0 {
		t.Fatalf("附件合并文本不应自动路由, 实际调用 %d 次", router.calls)
	}
	if len(ex.sentTexts) != 0 {
		t.Fatalf("附件合并文本不得直投当前会话: %v", ex.sentTexts)
	}
	if !b.console.Busy() {
		t.Fatal("应转待选菜单")
	}
	if b.QueuedFiles() != 1 {
		t.Fatalf("待选阶段队列应保留, 实际 %d", b.QueuedFiles())
	}
	sends := rc.sends()
	if len(sends) == 0 {
		t.Fatal("应回一条降级说明")
	}
	want := bridgeText("zh-CN", "err.router.attachment_needs_target")
	if got := sendText(t, sends[0].body); !strings.Contains(got, want) {
		t.Fatalf("降级说明应含原因 %q: %q", want, got)
	}
}

// 挂起反问回执带任务归因前缀，用户能看清回答将续跑哪条任务（修复 3）。
func TestBridgeManagerAskReplyAttribution(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
	b, rc := newRouteBridge(t, ex)
	b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
		return ManagerOutcome{
			Replies:     []string{"【问题1】交给哪个会话执行？\n1. 甲\n2. 乙"},
			Suspended:   true,
			Attribution: "分析 RPA 自动登录定时任务失败",
		}, true
	})

	b.HandleInbound("u1", "ct-1", "分析下 RPA 任务失败")
	sends := rc.sends()
	if len(sends) != 1 {
		t.Fatalf("应回显 1 条反问, 实际 %d", len(sends))
	}
	// deliver 会把 \n 升级成硬换行（行尾两空格），前缀断言去掉换行后再比。
	prefix := strings.TrimSuffix(fmt.Sprintf(bridgeText("zh-CN", "ask.attribution"), "分析 RPA 自动登录定时任务失败"), "\n")
	got := sendText(t, sends[0].body)
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("反问回执应带任务归因前缀 %q: %q", prefix, got)
	}
	if !strings.Contains(got, "【问题1】") {
		t.Fatalf("归因前缀不得吞掉问题正文: %q", got)
	}
}

// 非挂起的结论回执不加「待回答」前缀。
func TestBridgeManagerConclusionNoAttribution(t *testing.T) {
	ex := &fakeExec{hasCur: true, current: hub.SessionRef{ChatID: "c1", Title: "会话A"}}
	b, rc := newRouteBridge(t, ex)
	b.SetManagerRouter(func(context.Context, string) (ManagerOutcome, bool) {
		return ManagerOutcome{Replies: []string{"已完成。", ""}, Attribution: "不应使用"}, true
	})

	b.HandleInbound("u1", "ct-1", "干点什么")
	sends := rc.sends()
	if len(sends) != 2 {
		t.Fatalf("应逐条回显 2 条 Replies, 实际 %d", len(sends))
	}
	if got := sendText(t, sends[0].body); strings.HasPrefix(got, "【AgentHub") {
		t.Fatalf("非挂起结论不应加归因前缀: %q", got)
	}
}
