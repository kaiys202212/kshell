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