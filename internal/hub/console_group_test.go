package hub

import (
	"errors"
	"strings"
	"testing"
)

func groupConsole() (*Console, *fakeExecutor) {
	f := &fakeExecutor{}
	f.listSessions = []SessionRef{
		{Num: 1, ChatID: "c1", SessionID: "s1", Title: "A", Tool: "claude", Workspace: "/w1"},
		{Num: 2, ChatID: "c2", SessionID: "s2", Title: "B", Tool: "codex", Workspace: "/w2"},
		{Num: 3, ChatID: "c3", SessionID: "s3", Title: "C", Tool: "claude", Workspace: "/w3"},
	}
	c := NewConsole(f)
	c.lastList = f.listSessions // 模拟 #l 后的编号语境
	return c, f
}

func TestHandleGGroupSend(t *testing.T) {
	c, f := groupConsole()
	c.Handle("#g 1,2 hi")
	if len(f.sendCalls) != 2 || f.sendCalls[0] != [2]string{"c1", "hi"} || f.sendCalls[1] != [2]string{"c2", "hi"} {
		t.Fatalf("sendCalls = %+v", f.sendCalls)
	}
}

func TestHandleGSingleUnchanged(t *testing.T) {
	c, f := groupConsole()
	got := c.Handle("#g 1 hi")[0].Text
	if !strings.Contains(got, "【A】") {
		t.Fatalf("单发回执 = %q", got)
	}
	if len(f.sendCalls) != 1 || f.sendCalls[0] != [2]string{"c1", "hi"} {
		t.Fatalf("sendCalls = %+v", f.sendCalls)
	}
}

func TestHandleGInvalidNumberRejectsAll(t *testing.T) {
	c, f := groupConsole()
	got := c.Handle("#g 1,9 hi")[0].Text
	if got != c.text("no_mapping") {
		t.Fatalf("无效编号应整条拒绝回 no_mapping, got %q", got)
	}
	if len(f.sendCalls) != 0 {
		t.Fatalf("不得部分发送: %+v", f.sendCalls)
	}
}

func TestHandleGBadBodyRejects(t *testing.T) {
	c, f := groupConsole()
	tests := []string{"#g 1", "#g", "#g 1,2", "#g hi", "#g 1 2"}
	for _, in := range tests {
		got := c.Handle(in)[0].Text
		if got != c.text("usage_g") && got != c.text("no_mapping") {
			t.Fatalf("%q 拒绝文案 = %q", in, got)
		}
	}
	if len(f.sendCalls) != 0 {
		t.Fatalf("发送不应发生: %+v", f.sendCalls)
	}
}

func TestHandleGDedup(t *testing.T) {
	c, f := groupConsole()
	c.Handle("#g 1,1,2 hi")
	if len(f.sendCalls) != 2 {
		t.Fatalf("去重后应 2 次: %+v", f.sendCalls)
	}
}

func TestHandleGPartialFailure(t *testing.T) {
	c, f := groupConsole()
	c.lastList[2].ChatID = "" // 目标3未激活
	f.openSessionErr = errors.New("err.hub.terminal_active")
	got := c.Handle("#g 1,3 hi")[0].Text
	if !strings.Contains(got, "已发送") || !strings.Contains(got, "失败") {
		t.Fatalf("部分失败回执 = %q", got)
	}
	if len(f.sendCalls) != 1 || f.sendCalls[0][0] != "c1" {
		t.Fatalf("成功目标仍须发送: %+v", f.sendCalls)
	}
}

func TestGGroupEntersAggregateAndPreciseSendExits(t *testing.T) {
	c, f := groupConsole()
	c.Handle("#g 1,2 hi")
	if !c.agg.Active() || c.agg.Count() != 2 {
		t.Fatal("群发两目标应进聚合")
	}
	// 普通直投成功=精确投递,退出聚合。
	f.lastActivityID = 100
	f.lastActivityRef = f.listSessions[0]
	f.hasActivity = true
	c.Handle("再看一眼")
	if c.agg.Active() {
		t.Fatal("普通直投成功应清空聚合")
	}
	// 回菜单编号选择机制不干扰;再群发后单发退聚合。
	c.agg.Discard()
	c.Handle("#g 1,2 hi")
	c.Handle("#g 1 hi")
	if c.agg.Active() {
		t.Fatal("单发精确投递应清空聚合")
	}
}

func TestGGroupDoesNotTouchAttribution(t *testing.T) {
	c, f := groupConsole()
	f.lastActivityID = 7
	f.lastActivityRef = f.listSessions[1]
	f.hasActivity = true
	c.refreshCurrent()
	beforeMark := c.currentMark
	beforeTitle := c.current.Title
	c.Handle("#g 1,2 hi")
	if c.currentMark != beforeMark || c.current.Title != beforeTitle {
		t.Fatalf("群发不得改归因: mark=%d→%d title=%s→%s", beforeMark, c.currentMark, beforeTitle, c.current.Title)
	}
	if c.agg.Count() == 0 {
		t.Fatal("群发应登记聚合")
	}
}

func TestMirrorPolicyShared(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	p := NewMirrorPolicy()
	c.SetMirrorPolicy(p)
	if c.MirrorPolicy() != p {
		t.Fatal("应可共享替换")
	}
	c.SetMirrorPolicy(nil)
	if c.MirrorPolicy() != p {
		t.Fatal("nil 拒绝替换")
	}
}
