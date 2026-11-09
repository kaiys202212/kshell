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

// 出站镜像聚合的真集成:两 chat 两目标登记后,单目标 agent 回复逐条被折叠成
// 聚合摘要行(镜像侧收到合成行,store 仍全量落盘原文)。
func TestRecordHubMessageAggregated(t *testing.T) {
	a, store, rec := newHubTestApp(t)
	m := newChatManagerWith(rec.emit, fakeChatBackend{}, nil, nil, a.recordHubAgentMessage, nil, nil)
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
		case <-time.After(50 * time.Millisecond):
			return
		}
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
