package chat

import (
	"testing"
	"time"
)

// 状态变化必须经 onStatus 主动外推：前端即便丢失 turn_done 事件，也能从
// chat:status 收到 ready，不会永久停在 running。
func TestManagerStatusChangePushedViaOnStatus(t *testing.T) {
	m, b, _ := newTestManager(t)
	statuses := make(chan string, 32)
	m.SetOnStatus(func(id, st string, seq int64) {
		select {
		case statuses <- st:
		default:
		}
	})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b.conn.promptGate = make(chan struct{})
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)
	close(b.conn.promptGate)
	waitStatus(t, m, info.ID, StatusReady)

	got := map[string]int{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got[StatusRunning] > 0 && got[StatusReady] > 0 {
			return
		}
		select {
		case st := <-statuses:
			got[st]++
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatalf("onStatus 未收到 running+ready，实际=%v", got)
}

// prompt 永不回包时看门狗必须收口：状态回 ready、时间线补 turn_timeout；
// 之后迟到的真实响应不得重复收尾，也不得把状态拉回 running。
func TestTurnWatchdogExpiresStuckPrompt(t *testing.T) {
	old := turnWatchdogTimeout
	turnWatchdogTimeout = 150 * time.Millisecond
	t.Cleanup(func() { turnWatchdogTimeout = old })

	m, b, c, _ := newIdleTestManager(t)
	b.conn.promptGate = make(chan struct{}) // Prompt 卡住，模拟 agent 不回包
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)
	waitStatus(t, m, info.ID, StatusReady) // 看门狗收口

	if n := countTurnDone(c, "turn_timeout"); n != 1 {
		t.Fatalf("应恰好一条 turn_timeout turn_done，实际=%d", n)
	}
	// 放行迟到的真实响应：不得重复 turn_done、不得回 running。
	close(b.conn.promptGate)
	time.Sleep(150 * time.Millisecond)
	if n := countTurnDone(c, "end_turn"); n != 0 {
		t.Fatalf("迟到响应不得追加 end_turn turn_done，实际=%d", n)
	}
	if st, ok := m.Get(info.ID); !ok || st.Status != StatusReady {
		t.Fatalf("迟到响应后状态应保持 ready，实际=%+v", st)
	}
}

// 取消后 agent 不回包时，取消超时必须收口：状态回 ready、时间线补 cancelled，
// 之后迟到的真实响应不得重复收尾。
func TestCancelTimeoutFinishesTurn(t *testing.T) {
	oldCancel := cancelTimeout
	cancelTimeout = 150 * time.Millisecond
	t.Cleanup(func() { cancelTimeout = oldCancel })

	m, b, c, _ := newIdleTestManager(t)
	b.conn.promptGate = make(chan struct{}) // agent 收取消通知但不回包
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)

	if err := m.Cancel(info.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusReady)
	if n := countTurnDone(c, "cancelled"); n != 1 {
		t.Fatalf("取消超时应恰好一条 cancelled turn_done，实际=%d", n)
	}

	close(b.conn.promptGate)
	time.Sleep(150 * time.Millisecond)
	if n := countTurnDone(c, "cancelled"); n != 1 {
		t.Fatalf("迟到响应不得重复收尾，实际=%d", n)
	}
	if st, ok := m.Get(info.ID); !ok || st.Status != StatusReady {
		t.Fatalf("取消超时后状态应保持 ready，实际=%+v", st)
	}
}

// 在途工具永远等不到终态时，超过最大存活期必须合成 failed 收口并回 ready，
// 不能靠「有在途工具就永远不排兜底」把状态钉死。
func TestOrphanToolExpiresToReady(t *testing.T) {
	old := orphanToolTimeout
	orphanToolTimeout = 180 * time.Millisecond
	t.Cleanup(func() { orphanToolTimeout = old })

	m, b, c, _ := newIdleTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)
	// 子任务工具在途；prompt 提前返回 end_turn（codebuddy 形态）
	b.last.SessionUpdate("sess-new", []byte(`{"sessionUpdate":"tool_call","toolCallId":"sub1","name":"Task","status":"in_progress"}`))
	close(b.conn.promptGate)
	// turn_done 已发但不得立即回 ready
	waitTurnDone(t, c)
	if st, _ := m.Get(info.ID); st.Status != StatusRunning {
		t.Fatalf("在途工具期间不应回 ready: %+v", st)
	}
	// 超过最大存活期：合成 failed 终态并回 ready
	waitStatus(t, m, info.ID, StatusReady)
	if !hasToolStatus(c, "sub1", "failed") {
		t.Fatal("孤儿工具应收口为 failed")
	}
}

// hasToolStatus 报告时间线里是否出现过指定工具与状态的更新。
func hasToolStatus(c *collector, toolID, status string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, u := range c.updates {
		if u.Type == "tool" && u.ToolCallID == toolID && u.Tool != nil && u.Tool.Status == status {
			return true
		}
	}
	return false
}

// 同 key 的旧会话若连接已死（进程没了但 watchExit 尚未标记 exited），Open 必须
// 重建而不是把陈旧 running 快照返回给调用方。
func TestOpenRebuildsWhenConnDead(t *testing.T) {
	m, b, _ := newTestManager(t)
	first, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b.conn.mu.Lock()
	b.conn.dead = true // 连接已不可用，但 Wait 未返回（模拟 watchExit 未完成）
	b.conn.mu.Unlock()

	out, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == first.ID {
		t.Fatalf("连接已死应重建，实际仍复用旧会话 %s", out.ID)
	}
}

// countTurnDone 统计时间线中指定 stopReason 的 turn_done 数量。
func countTurnDone(c *collector, stop string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, u := range c.updates {
		if u.Type == "turn_done" && (stop == "" || u.StopReason == stop) {
			n++
		}
	}
	return n
}

// 过期（上一轮）的看门狗回调绝不能停掉当前轮的看门狗：否则新一轮无任何出口，
// 「永久运行中」在下一轮复现。守卫失败分支只允许静默返回。
func TestStaleWatchdogCallbackKeepsCurrentTimer(t *testing.T) {
	old := turnWatchdogTimeout
	turnWatchdogTimeout = 1 * time.Second
	t.Cleanup(func() { turnWatchdogTimeout = old })

	m, b, _, _ := newIdleTestManager(t)
	b.conn.promptGate = make(chan struct{})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, info.ID, StatusRunning)

	m.mu.Lock()
	s := m.byID[info.ID]
	stale := s.promptEpoch - 1 // 上一轮的 epoch
	m.mu.Unlock()
	m.expireTurn(s, stale, "turn_timeout", true) // 模拟迟到回调

	m.mu.Lock()
	armed := s.turnTimer != nil
	inFlight := s.promptInFlight
	m.mu.Unlock()
	if !inFlight {
		t.Fatal("过期回调不得影响当前轮的在途状态")
	}
	if !armed {
		t.Fatal("过期回调不得停掉当前轮的看门狗定时器")
	}

	// 当前轮看门狗仍须能正常收口。
	waitStatus(t, m, info.ID, StatusReady)
}
