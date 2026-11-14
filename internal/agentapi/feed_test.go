package agentapi

import (
	"testing"
	"time"
)

func TestFeedSubscribeReceive(t *testing.T) {
	f := NewFeed()
	ch, cancel := f.Subscribe("chat-a", 8)
	defer cancel()
	f.Publish(Event{SessionID: "chat-a", Kind: EventUpdate, Type: "assistant", Text: "hi"})
	select {
	case e := <-ch:
		if e.Text != "hi" || e.Kind != EventUpdate {
			t.Fatalf("收到错误事件: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到事件")
	}
}

func TestFeedCancelStopsDelivery(t *testing.T) {
	f := NewFeed()
	ch, cancel := f.Subscribe("chat-a", 8)
	cancel()
	// cancel 关闭通道；Publish 不应投递也不应 panic。
	f.Publish(Event{SessionID: "chat-a", Kind: EventUpdate})
	if _, ok := <-ch; ok {
		t.Fatal("cancel 后通道应关闭")
	}
}

func TestFeedDropWhenFull(t *testing.T) {
	f := NewFeed()
	ch, cancel := f.Subscribe("chat-a", 1)
	defer cancel()
	// 缓冲 1：先塞满，再发两帧均应非阻塞返回（后帧丢弃）。
	f.Publish(Event{SessionID: "chat-a", Kind: EventUpdate, Text: "1"})
	f.Publish(Event{SessionID: "chat-a", Kind: EventUpdate, Text: "2"})
	select {
	case e := <-ch:
		if e.Text != "1" {
			t.Fatalf("首帧应为 1: %+v", e)
		}
	default:
		t.Fatal("缓冲应已有首帧")
	}
}

func TestFeedPendingPermissions(t *testing.T) {
	f := NewFeed()
	f.Publish(Event{SessionID: "chat-a", Kind: EventPermission, Type: "req-1", Text: "Bash", Ts: 1})
	f.Publish(Event{SessionID: "chat-a", Kind: EventPermission, Type: "req-2", Text: "Write", Ts: 2})
	got := f.PendingPermissions("chat-a")
	if len(got) != 2 || got[0].RequestID != "req-1" || got[1].Title != "Write" {
		t.Fatalf("pending 快照不符: %+v", got)
	}
	f.ClearPermission("chat-a", "req-1")
	got = f.PendingPermissions("chat-a")
	if len(got) != 1 || got[0].RequestID != "req-2" {
		t.Fatalf("清除后不符: %+v", got)
	}
	if n := len(f.PendingPermissions("chat-zz")); n != 0 {
		t.Fatalf("未知会话应返回空: %d", n)
	}
}

func TestFeedConcurrent(t *testing.T) {
	f := NewFeed()
	ch, cancel := f.Subscribe("chat-a", 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			f.Publish(Event{SessionID: "chat-a", Kind: EventUpdate, Text: "x"})
			f.ClearPermission("chat-a", "r")
			_ = f.PendingPermissions("chat-a")
		}
	}()
	for range 500 {
		select {
		case <-ch:
		default:
		}
	}
	cancel()
	<-done
}
