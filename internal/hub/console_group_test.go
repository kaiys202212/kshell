package hub

import (
	"errors"
	"fmt"
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
	// 精确断言 usage_g：有编号但正文缺失/被吞。
	for _, in := range []string{"#g", "#g 1", "#g 1,2", "#g 1 2"} {
		if got := c.Handle(in)[0].Text; got != c.text("usage_g") {
			t.Fatalf("%q 应精确回 usage_g, got %q", in, got)
		}
	}
	// 粗断言兜底：正文为空等模糊形态仍落 usage_g/no_mapping 之一（此处 "#g hi"
	// 编号段为空，也 usage_g）。
	for _, in := range []string{"#g hi"} {
		got := c.Handle(in)[0].Text
		if got != c.text("usage_g") && got != c.text("no_mapping") {
			t.Fatalf("%q 拒绝文案 = %q", in, got)
		}
	}
	if len(f.sendCalls) != 0 {
		t.Fatalf("发送不应发生: %+v", f.sendCalls)
	}
	// 旧行为守护：编号段后全角冒号兼容仍然成立。
	c2, f2 := groupConsole()
	c2.Handle("#g 1：hi")
	if len(f2.sendCalls) != 1 || f2.sendCalls[0] != [2]string{"c1", "hi"} {
		t.Fatalf("全角冒号兼容破坏: %+v", f2.sendCalls)
	}
}

func TestHandleGFullwidthComma(t *testing.T) {
	c, f := groupConsole()
	c.Handle("#g 1，2 hi")
	if len(f.sendCalls) != 2 || f.sendCalls[0] != [2]string{"c1", "hi"} || f.sendCalls[1] != [2]string{"c2", "hi"} {
		t.Fatalf("全角逗号群发 sendCalls = %+v", f.sendCalls)
	}
	// 全角逗号后非数字：接正文则拒绝，不发送。
	c2, f2 := groupConsole()
	got := c2.Handle("#g 1，hi")[0].Text
	if got != c2.text("usage_g") && got != c2.text("no_mapping") {
		t.Fatalf("全角逗号后非数字应拒绝, got %q", got)
	}
	if len(f2.sendCalls) != 0 {
		t.Fatalf("不得发送: %+v", f2.sendCalls)
	}
}

func TestHandleGSingleFailure(t *testing.T) {
	c, f := groupConsole()
	c.lastList[0].ChatID = ""
	c.lastList[0].SessionID = "s1"
	f.openSessionErr = errors.New("err.hub.terminal_active")
	got := c.Handle("#g 1 hi")[0].Text
	// errText 行为：wire key 会翻译成中文原文；按行为断言而非硬编码文案。
	want := c.text("err.hub.terminal_active")
	if !strings.Contains(got, want) {
		t.Fatalf("单目标失败应透传原因 %q, got %q", want, got)
	}
	if strings.Contains(got, c.text("send_failed")) {
		t.Fatalf("有原因时不得兜底 send_failed: %q", got)
	}
	if len(f.sendCalls) != 0 {
		t.Fatalf("失败目标不得发送: %+v", f.sendCalls)
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

// TestSendGroupTexts 覆盖公开群发入口的 #g 同契约语义：refs 直传（不依赖
// lastList 编号）、texts 与 refs 一一对应、去重/拦截/激活/部分失败/聚合/不改
// 归因均与 handleG 一致。
func TestSendGroupTexts(t *testing.T) {
	t.Run("两目标各收各自文本并进聚合", func(t *testing.T) {
		c, f := groupConsole()
		r1, r2 := f.listSessions[0], f.listSessions[1]
		replies := c.SendGroupTexts([]SessionRef{r1, r2}, []string{"t1", "t2"})
		if len(f.sendCalls) != 2 ||
			f.sendCalls[0] != [2]string{"c1", "t1"} ||
			f.sendCalls[1] != [2]string{"c2", "t2"} {
			t.Fatalf("sendCalls = %+v（文本须与 refs 一一对应）", f.sendCalls)
		}
		want := fmt.Sprintf(c.text("group_sent"), "1【A】 2【B】")
		if len(replies) != 1 || replies[0].Text != want {
			t.Fatalf("群发回执 = %+v, want %q", replies, want)
		}
		if !c.MirrorPolicy().Active() || len(c.MirrorPolicy().Targets()) != 2 {
			t.Fatalf("两目标成功应进聚合: active=%v targets=%d",
				c.MirrorPolicy().Active(), len(c.MirrorPolicy().Targets()))
		}
	})

	t.Run("单ref等价单发语义", func(t *testing.T) {
		c, f := groupConsole()
		r1 := f.listSessions[0]
		replies := c.SendGroupTexts([]SessionRef{r1}, []string{"hi"})
		want := fmt.Sprintf(c.text("sent_to"), sessionTag("A"))
		if len(replies) != 1 || replies[0].Text != want {
			t.Fatalf("单发回执 = %+v, want %q", replies, want)
		}
		if len(f.sendCalls) != 1 || f.sendCalls[0] != [2]string{"c1", "hi"} {
			t.Fatalf("sendCalls = %+v", f.sendCalls)
		}
		if c.MirrorPolicy().Active() {
			t.Fatal("单发属精确投递，应退出聚合")
		}
	})

	t.Run("未激活目标先OpenSession再Send", func(t *testing.T) {
		c, f := groupConsole()
		r3 := f.listSessions[2]
		r3.ChatID = "" // 未激活的磁盘会话
		f.openSessionRef = SessionRef{
			Num: 3, ChatID: "c3-open", SessionID: "s3", Title: "C",
			Tool: "claude", Workspace: "/w3",
		}
		replies := c.SendGroupTexts([]SessionRef{r3}, []string{"hi"})
		if len(f.openSessionCalls) != 1 || f.openSessionCalls[0] != "s3" {
			t.Fatalf("openSessionCalls = %+v", f.openSessionCalls)
		}
		if len(f.sendCalls) != 1 || f.sendCalls[0] != [2]string{"c3-open", "hi"} {
			t.Fatalf("激活后应发往新 ChatID: %+v", f.sendCalls)
		}
		want := fmt.Sprintf(c.text("sent_to"), sessionTag("C"))
		if len(replies) != 1 || replies[0].Text != want {
			t.Fatalf("回执 = %+v, want %q", replies, want)
		}
	})

	t.Run("终端占用目标失败但不中断其他", func(t *testing.T) {
		c, f := groupConsole()
		term := f.listSessions[0]
		term.TerminalActive = true
		r2 := f.listSessions[1]
		replies := c.SendGroupTexts([]SessionRef{term, r2}, []string{"t1", "t2"})
		if len(f.sendCalls) != 1 || f.sendCalls[0] != [2]string{"c2", "t2"} {
			t.Fatalf("终端目标须跳过、其余照发: %+v", f.sendCalls)
		}
		if len(replies) != 1 {
			t.Fatalf("应回单条回执, got %+v", replies)
		}
		got := replies[0].Text
		if !strings.Contains(got, c.text("err.hub.terminal_active")) {
			t.Fatalf("回执应含 terminal_active 文案: %q", got)
		}
		if !strings.Contains(got, "2【B】") {
			t.Fatalf("成功目标应列入 group_sent: %q", got)
		}
	})

	t.Run("Send失败的目标记入回执", func(t *testing.T) {
		c, f := groupConsole()
		r1, r2 := f.listSessions[0], f.listSessions[1]
		f.sendErr = errors.New("boom")
		replies := c.SendGroupTexts([]SessionRef{r1, r2}, []string{"t1", "t2"})
		if len(replies) != 1 {
			t.Fatalf("应回单条回执, got %+v", replies)
		}
		got := replies[0].Text
		if !strings.Contains(got, "boom") {
			t.Fatalf("失败原因应透传: %q", got)
		}
		failPrefix := strings.SplitN(c.text("group_failed"), "%s", 2)[0]
		if !strings.Contains(got, failPrefix) {
			t.Fatalf("回执应为 group_failed 形态: %q", got)
		}
		if c.MirrorPolicy().Active() {
			t.Fatal("全失败不得进入聚合")
		}
	})

	t.Run("不改current不落归因水位", func(t *testing.T) {
		c, f := groupConsole()
		f.lastActivityID = 7
		f.lastActivityRef = f.listSessions[1]
		f.hasActivity = true
		c.refreshCurrent()
		beforeMark := c.currentMark
		beforeTitle := c.current.Title
		r1, r2 := f.listSessions[0], f.listSessions[1]
		c.SendGroupTexts([]SessionRef{r1, r2}, []string{"t1", "t2"})
		if c.currentMark != beforeMark || c.current.Title != beforeTitle {
			t.Fatalf("群发不得改归因: mark=%d→%d title=%s→%s",
				beforeMark, c.currentMark, beforeTitle, c.current.Title)
		}
		if c.MirrorPolicy().Count() != 2 {
			t.Fatalf("群发应登记聚合: count=%d", c.MirrorPolicy().Count())
		}
	})

	// 公开入口防御，handleG 恒等长。
	t.Run("texts过短防御性钳制", func(t *testing.T) {
		c, f := groupConsole()
		r1, r2 := f.listSessions[0], f.listSessions[1]
		c.SendGroupTexts([]SessionRef{r1, r2}, []string{"only"})
		if len(f.sendCalls) != 1 || f.sendCalls[0] != [2]string{"c1", "only"} {
			t.Fatalf("应只投递较短长度: %+v", f.sendCalls)
		}
	})

	t.Run("refs为空返回nil", func(t *testing.T) {
		c, f := groupConsole()
		replies := c.SendGroupTexts(nil, nil)
		if len(replies) != 0 {
			t.Fatalf("refs 为空应返回 nil(不产生回执), got %+v", replies)
		}
		if len(f.sendCalls) != 0 {
			t.Fatalf("不应发送: %+v", f.sendCalls)
		}
	})

	t.Run("去重连带丢弃重复项文案", func(t *testing.T) {
		c, f := groupConsole()
		r1, r2 := f.listSessions[0], f.listSessions[1]
		replies := c.SendGroupTexts([]SessionRef{r1, r1, r2}, []string{"a", "b", "c"})
		if len(f.sendCalls) != 2 ||
			f.sendCalls[0] != [2]string{"c1", "a"} ||
			f.sendCalls[1] != [2]string{"c2", "c"} {
			t.Fatalf("去重后重复项文案应一并丢弃: %+v", f.sendCalls)
		}
		want := fmt.Sprintf(c.text("group_sent"), "1【A】 2【B】")
		if len(replies) != 1 || replies[0].Text != want {
			t.Fatalf("群发回执 = %+v, want %q", replies, want)
		}
	})
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
