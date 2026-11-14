package agentapi

import (
	"errors"
	"testing"
	"time"
)

func TestControlWrappers(t *testing.T) {
	f := newFake()
	api := New(f)

	if err := api.Send("sess-a", "你好"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-a|你好" {
		t.Fatalf("Send 应按 SessionID 归一到 chatID: %v", f.sent)
	}
	if err := api.Stop("chat-b"); err != nil || len(f.stopped) != 1 || f.stopped[0] != "chat-b" {
		t.Fatalf("Stop: %v %v", err, f.stopped)
	}
	if err := api.Recall(42); err != nil || len(f.recalled) != 1 || f.recalled[0] != 42 {
		t.Fatalf("Recall: %v %v", err, f.recalled)
	}
	models, cur, err := api.Models("chat-a")
	if err != nil || cur != f.curModel || len(models) != len(f.models) {
		t.Fatalf("Models: %v", err)
	}
	if err := api.SetModel("chat-a", "m1"); err != nil || f.setModels[0][0] != "chat-a" || f.setModels[0][1] != "m1" {
		t.Fatalf("SetModel: %v %v", err, f.setModels)
	}
	if err := api.GrantPermission("chat-a", "req-9", true); err != nil || f.grants[0].allow != true {
		t.Fatalf("GrantPermission: %v", err)
	}
	// 未知会话统一 ErrNotFound。
	if err := api.Send("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知会话 Send 期望 ErrNotFound: %v", err)
	}
	if err := api.Stop("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知会话 Stop 期望 ErrNotFound: %v", err)
	}
}

func TestWatch(t *testing.T) {
	f := newFake()
	api := New(f)
	ch, cancel := api.Watch("chat-a", 8)
	defer cancel()
	f.Feed().Publish(Event{SessionID: "chat-a", Kind: EventUpdate, Type: "assistant", Text: "chunk"})
	select {
	case e := <-ch:
		if e.Text != "chunk" {
			t.Fatalf("Watch 收到错误事件: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("Watch 未收到事件")
	}
	// 未知会话。
	if _, cancel2 := api.Watch("nope", 8); cancel2 == nil {
		t.Log("未知会话 Watch 行为按实现定义")
	} else {
		cancel2()
	}
}

func TestPendingPermissions(t *testing.T) {
	f := newFake()
	api := New(f)
	nowNano := int64(12345)
	f.Feed().Publish(Event{SessionID: "chat-a", Kind: EventPermission, Type: "req-1", Text: "Bash: ls", Ts: nowNano})
	got := api.PendingPermissions("chat-a")
	if len(got) != 1 || got[0].RequestID != "req-1" || got[0].Title != "Bash: ls" {
		t.Fatalf("PendingPermissions 不符: %+v", got)
	}
	// GrantPermission 成功后从快照清除。
	if err := api.GrantPermission("chat-a", "req-1", true); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
	if n := len(api.PendingPermissions("chat-a")); n != 0 {
		t.Fatalf("授权后 pending 应清空: %d", n)
	}
}
