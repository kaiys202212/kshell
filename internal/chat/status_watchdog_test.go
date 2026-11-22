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
	m.SetOnStatus(func(id, st string) {
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