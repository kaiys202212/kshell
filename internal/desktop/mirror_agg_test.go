package desktop

import (
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/weixin"
)

// composeAggLine 的组词与形态:agg_line 中文骨架、逐目标状态词、
// perm 明细带 40 截断摘要。断言用字面中文(与 lang.go 原文同源)。
func TestComposeAggLine(t *testing.T) {
	rows := []hub.SessionStatus{
		{Ref: hub.SessionRef{Num: 1, Tool: "claude", Title: "A"}, State: "idle", Summary: "完成修复"},
		{Ref: hub.SessionRef{Num: 2, Tool: "codex", Title: "B"}, State: "running"},
		{Ref: hub.SessionRef{Num: 3, Tool: "claude", Title: "C"}, State: "perm", Summary: "请回复 1(允许)"},
		{Ref: hub.SessionRef{Num: 4, Tool: "codex", Title: "D"}, State: "gone"},
	}
	got := composeAggLine("zh-CN", rows, 2, 4)
	for _, want := range []string{
		"2/4 会话有回复",
		"1【A】已完成",
		"2【B】运行中",
		"3【C】权限待办",
		"4【D】已断开",
		// perm 明细带截到 40 的摘要(短文不截断,原样出现)
		truncateFirstLine("请回复 1(允许)", 40),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺 %q: %q", want, got)
		}
	}
}

// 无标题会话的标签形态:pseudo 行只带编号(编号语境由调用方在渲染时编号)。
func TestComposeAggLineNoTitle(t *testing.T) {
	rows := []hub.SessionStatus{
		{Ref: hub.SessionRef{Num: 1, Tool: "claude"}, State: "idle"},
		{Ref: hub.SessionRef{Num: 2, Tool: "codex"}, State: "perm", Summary: "悬"},
	}
	got := composeAggLine("zh-CN", rows, 0, 2)
	if !strings.Contains(got, "0/2 会话有回复") {
		t.Fatalf("骨架不符: %q", got)
	}
	if !strings.Contains(got, "1 已完成") || !strings.Contains(got, "2 权限待办 — 悬") {
		t.Fatalf("无标题标签/摘要不符: %q", got)
	}
}

// 聚合用例公用 helper:开一个会话并等握手到 Ready(statusOf(ready)=idle,
// 才可能满足「全终态」)。
func openReadyChat(t *testing.T, m *chat.Manager, key, ws, title, sid string) chat.Info {
	t.Helper()
	info, err := m.Open(key, chat.Info{
		Kind: chat.KindSession, Workspace: ws, Title: title, ToolID: "claude",
	}, chat.Spec{Path: "x"}, sid)
	if err != nil {
		t.Fatalf("open %s: %v", key, err)
	}
	wxWaitStatus(t, m, info.ID, chat.StatusReady)
	return info
}

// 聚合用例公用 helper:装镜像捕获桩(sink 收 OnHubMessage)并登记目标集
// 与 since 水位,返回 sink 供断言。
func enterAggPolicy(t *testing.T, a *App, since int64, refs ...hub.SessionRef) chan hub.Message {
	t.Helper()
	sink := make(chan hub.Message, 8)
	r := weixin.NewRuntime(newWXExecutor(a), "zh-CN")
	r.SetHubSink(func(mm hub.Message) { sink <- mm })
	a.mu.Lock()
	a.wx = r
	a.mirrorPolicy = hub.NewMirrorPolicy()
	a.mirrorPolicy.Enter(refs)
	a.mirrorPolicy.EnterSinceTs(since)
	a.mu.Unlock()
	return sink
}

// 出站镜像聚合的真集成:两 chat 两目标登记后,单目标 agent 回复逐条被折叠成
// 聚合摘要行(镜像侧收到合成行,store 仍全量落盘原文)。
func TestRecordHubMessageAggregated(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	// 两目标持续 running:不触发全终态汇报(T8),本用例专测 agg_line 折叠路径。
	b := &wxStatusBackend{gate: make(chan struct{})}
	defer close(b.gate)
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info1, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w1", Title: "A", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	info2, err := m.Open("session:s2", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w2", Title: "B", ToolID: "claude",
	}, chat.Spec{Path: "y"}, "s2")
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	// 握手完成后各起一轮并阻塞在 gate,稳定停在 running。
	wxWaitStatus(t, m, info1.ID, chat.StatusReady)
	wxWaitStatus(t, m, info2.ID, chat.StatusReady)
	go func() { _ = m.Prompt(info1.ID, "跑一轮") }()
	go func() { _ = m.Prompt(info2.ID, "跑一轮") }()
	wxWaitStatus(t, m, info1.ID, chat.StatusRunning)
	wxWaitStatus(t, m, info2.ID, chat.StatusRunning)

	// 镜像捕获桩:OnHubMessage 不进 bridge 出站队列,收进切片。
	sink := make(chan hub.Message, 8)
	r := weixin.NewRuntime(newWXExecutor(a), "zh-CN")
	r.SetHubSink(func(mm hub.Message) { sink <- mm })
	a.mu.Lock()
	a.wx = r
	a.mirrorPolicy = hub.NewMirrorPolicy()
	a.mirrorPolicy.Enter([]hub.SessionRef{
		{ChatID: info1.ID, SessionID: "s1", Title: "A"},
		{ChatID: info2.ID, SessionID: "s2", Title: "B"},
	})
	// 时间基准钉死在消息落盘之前:两条都在边界后,均计入 replied。
	a.mirrorPolicy.EnterSinceTs(time.Now().UnixNano() - 1)
	a.mu.Unlock()

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "第一步完成",
	})
	select {
	case got := <-sink:
		if !strings.Contains(got.Text, "1/2 会话有回复") {
			t.Fatalf("首条摘要骨架不符: %q", got.Text)
		}
		if !strings.Contains(got.Text, "【A】") || !strings.Contains(got.Text, "【B】") {
			t.Fatalf("摘要应含两目标标签: %q", got.Text)
		}
		// 非原文:B 端不应收到claude 的逐字回复。
		if strings.Contains(got.Text, "第一步完成") {
			t.Fatalf("摘要不应含原文: %q", got.Text)
		}
	default:
		t.Fatal("第一条 agent 消息应出站聚合摘要行")
	}

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info2.ID, TermKey: info2.Key, Summary: "第二步完成",
	})
	select {
	case got := <-sink:
		if !strings.Contains(got.Text, "2/2 会话有回复") {
			t.Fatalf("第二条摘要骨架不符: %q", got.Text)
		}
	default:
		t.Fatal("第二条 agent 消息应出站聚合摘要行")
	}

	// store 全量:两条原文都在(聚合只影响出站镜像,不入历史)。
	msgs := store.List()
	orig := 0
	for _, mm := range msgs {
		if mm.Kind == "agent" && (mm.Text == "第一步完成" || mm.Text == "第二步完成") {
			orig++
		}
	}
	if orig != 2 {
		t.Fatalf("store 应保留两条原文,得到 %d: %+v", orig, msgs)
	}

	// 聚合中的会话回复不再逐条镜像:再次往 A 发,仍是聚合计数,不再出摘要?
	// 出站摘要行每次目标消息都重发(计数推进可见),只断言不落原文镜像。
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "第三步完成",
	})
	// 由于所有目标都有回复,聚合计数推进到 2/2(目标集就是 2 个,不会涨)。
	// 摘要行仍出站;若未来改为 Sent: 已发送语义需另改断言。
	for {
		select {
		case got := <-sink:
			if strings.Contains(got.Text, "第三步完成") {
				t.Fatalf("聚合后不应镜像原文: %q", got.Text)
			}
			// 50ms 即可:HubSink 是同步调用,聚合摘要已在 select 前投进
			// 缓冲 8 的 sink,drain 只兜不预期的原文镜像。
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// 全终态自动汇报:两目标均已回复、且均无 running/perm 时,触发消息的出站改投
// 汇总报告(report_header 明细 + report_hint),而非 agg_line 摘要行。报告即
// 终点:策略 Discard,其后的 agent 消息恢复逐条原文镜像。
func TestMirrorAggregatedReportOnAllTerminal(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	// 等握手完成(Starting→Ready):statusOf(ready)=idle,才满足「全终态」。
	info1 := openReadyChat(t, m, "session:s1", "D:/w1", "A", "s1")
	info2 := openReadyChat(t, m, "session:s2", "D:/w2", "B", "s2")

	since := time.Now().UnixNano()
	sink := enterAggPolicy(t, a, since,
		hub.SessionRef{ChatID: info1.ID, SessionID: "s1", Title: "A"},
		hub.SessionRef{ChatID: info2.ID, SessionID: "s2", Title: "B"},
	)

	// 预置两目标的回复水位(直接落 store,不触发镜像):LastTs>=since → 均计入
	// replied;两 chat 均已 ready(idle),故全终态成立。
	for _, info := range []chat.Info{info1, info2} {
		if err := store.Append(hub.Message{
			Kind: "agent", ChatID: a.originalChatID(info.ID), TermKey: info.Key, Text: "预置回复", Ts: since + 1,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// 触发消息:全终态且已回复 → 汇总报告,而非 agg_line 摘要行。
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "收尾",
	})
	select {
	case got := <-sink:
		if strings.Contains(got.Text, "会话有回复") {
			t.Fatalf("全终态应出报告而非摘要行: %q", got.Text)
		}
		for _, want := range []string{"全部完成", "1【A】", "2【B】", "#gx"} {
			if !strings.Contains(got.Text, want) {
				t.Fatalf("报告缺 %q: %q", want, got.Text)
			}
		}
	default:
		t.Fatal("全终态应出站汇总报告")
	}
	if a.mirrorPolicy.Active() {
		t.Fatal("报告后应 Discard 结束聚合周期")
	}

	// 报告即终点:后续 agent 消息恢复逐条原文镜像。
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "后续原文",
	})
	select {
	case got := <-sink:
		if got.Text != "后续原文" {
			t.Fatalf("Discard 后应恢复逐条镜像,得到 %q", got.Text)
		}
	default:
		t.Fatal("Discard 后应逐条镜像原文")
	}
}

// 反例:全终态但 replied==0(首轮回复水位未过 since,如触发消息正卡在边界):
// 吞掉触发消息,不发空骨架摘要行、不出报告,也不 Discard(周期仍在,等水位
// 推进后仍会正常汇总)。
func TestMirrorAggregatedSwallowWhenNoReplyYet(t *testing.T) {
	a, _, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info1 := openReadyChat(t, m, "session:s1", "D:/w1", "A", "s1")
	info2 := openReadyChat(t, m, "session:s2", "D:/w2", "B", "s2")

	// since 钉在 1h 后:两目标均无 LastTs>=since 的回复,replied==0;两 chat
	// 均 ready(idle),全终态本可成立,但先被 replied 守门拦下。
	sink := enterAggPolicy(t, a, time.Now().UnixNano()+int64(time.Hour),
		hub.SessionRef{ChatID: info1.ID, SessionID: "s1", Title: "A"},
		hub.SessionRef{ChatID: info2.ID, SessionID: "s2", Title: "B"},
	)

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "首条",
	})
	// HubSink 是同步调用,50ms 未出站即确认吞行(与既有用例同一时序约定)。
	select {
	case got := <-sink:
		t.Fatalf("replied==0 应吞行,却出站了 %q", got.Text)
	case <-time.After(50 * time.Millisecond):
	}
	if !a.mirrorPolicy.Active() {
		t.Fatal("吞行不应 Discard,聚合周期应仍在")
	}
}

// 反例:任一目标 running → 未全终态,仍出 agg_line 摘要行,不 Discard。
func TestMirrorAggregatedNoReportWhileRunning(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	// 阻塞 Prompt 的假后端:让甲会话稳定停在 running。
	b := &wxStatusBackend{gate: make(chan struct{})}
	defer close(b.gate)
	m := newChatManagerWith(rec.emit, b, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info1 := openReadyChat(t, m, "session:s1", "D:/w1", "A", "s1")
	info2 := openReadyChat(t, m, "session:s2", "D:/w2", "B", "s2")

	// 甲发起一轮并阻塞在 gate:state=running,报告条件不成立。
	go func() { _ = m.Prompt(info1.ID, "跑一轮") }()
	wxWaitStatus(t, m, info1.ID, chat.StatusRunning)

	since := time.Now().UnixNano()
	sink := enterAggPolicy(t, a, since,
		hub.SessionRef{ChatID: info1.ID, SessionID: "s1", Title: "A"},
		hub.SessionRef{ChatID: info2.ID, SessionID: "s2", Title: "B"},
	)

	// 预置乙的回复(replied>0),排除「0 回复吞行」分支,专测 running 阻断报告。
	if err := store.Append(hub.Message{
		Kind: "agent", ChatID: a.originalChatID(info2.ID), TermKey: info2.Key, Text: "预置回复", Ts: since + 1,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info1.ID, TermKey: info1.Key, Summary: "进行中",
	})
	select {
	case got := <-sink:
		if !strings.Contains(got.Text, "会话有回复") {
			t.Fatalf("有 running 时应出摘要行: %q", got.Text)
		}
		if strings.Contains(got.Text, "全部完成") {
			t.Fatalf("有 running 时不应出完成报告: %q", got.Text)
		}
	default:
		t.Fatal("有 running 时应出 agg_line 摘要行")
	}
	if !a.mirrorPolicy.Active() {
		t.Fatal("未全终态不应 Discard")
	}
}

// 反例:目标 perm(等待授权)不是完成态——无 running 但有 perm 时仍出摘要行,
// 不 Discard。perm 是「卡住待人」,全 perm 出「全部完成」会误导用户。
func TestMirrorAggregatedNoReportWhilePerm(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info1 := openReadyChat(t, m, "session:s1", "D:/w1", "A", "s1")
	info2 := openReadyChat(t, m, "session:s2", "D:/w2", "B", "s2")

	since := time.Now().UnixNano()
	sink := enterAggPolicy(t, a, since,
		hub.SessionRef{ChatID: info1.ID, SessionID: "s1", Title: "A"},
		hub.SessionRef{ChatID: info2.ID, SessionID: "s2", Title: "B"},
	)

	// 乙预置回复,保证 replied>0(不落吞行分支)。
	if err := store.Append(hub.Message{
		Kind: "agent", ChatID: a.originalChatID(info2.ID), TermKey: info2.Key, Text: "预置回复", Ts: since + 1,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// 甲的触发消息本身是权限请求(Event=Notification):SessionStatuses 判定 perm。
	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Notification", ChatID: info1.ID, TermKey: info1.Key, Summary: "请回复 1(允许)",
	})
	select {
	case got := <-sink:
		if !strings.Contains(got.Text, "会话有回复") {
			t.Fatalf("有 perm 时应出摘要行: %q", got.Text)
		}
		if strings.Contains(got.Text, "全部完成") {
			t.Fatalf("有 perm 时不应出完成报告: %q", got.Text)
		}
		if !strings.Contains(got.Text, "权限待办") {
			t.Fatalf("摘要明细应标出权限待办: %q", got.Text)
		}
	default:
		t.Fatal("有 perm 时应出 agg_line 摘要行")
	}
	if !a.mirrorPolicy.Active() {
		t.Fatal("有 perm 未全终态,不应 Discard")
	}
}

// 策略不匹配(从未登记的 chat 回复)时按现状逐条镜像,不吞、不聚合。
func TestRecordHubMessageAggregationNotActive(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
	t.Cleanup(m.CloseAll)
	a.opts.Chats = m

	info, err := m.Open("session:s1", chat.Info{
		Kind: chat.KindSession, Workspace: "D:/w", Title: "t", ToolID: "claude",
	}, chat.Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	sink := make(chan hub.Message, 4)
	r := weixin.NewRuntime(newWXExecutor(a), "zh-CN")
	r.SetHubSink(func(mm hub.Message) { sink <- mm })
	a.mu.Lock()
	a.wx = r
	a.mirrorPolicy = hub.NewMirrorPolicy() // 从未 Enter:不活跃
	a.mu.Unlock()

	a.recordHubAgentMessage(agenthook.Payload{
		Tool: "claude", Event: "Stop", ChatID: info.ID, TermKey: info.Key, Summary: "原文回复",
	})
	select {
	case got := <-sink:
		if got.Text != "原文回复" {
			t.Fatalf("非聚合应镜像原文,得到 %q", got.Text)
		}
	default:
		t.Fatal("策略不活跃时应逐条镜像原文")
	}
	if len(store.List()) != 1 {
		t.Fatalf("store 应有 1 条,得到 %d", len(store.List()))
	}
}
