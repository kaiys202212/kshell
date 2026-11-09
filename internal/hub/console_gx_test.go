package hub

import (
	"fmt"
	"strings"
	"testing"
)

func gxConsole() (*Console, *fakeExecutor) {
	f := &fakeExecutor{}
	f.listSessions = []SessionRef{
		{Num: 1, ChatID: "c1", SessionID: "s1", Title: "A", Tool: "claude", Workspace: "/w1"},
		{Num: 2, ChatID: "c2", SessionID: "s2", Title: "B", Tool: "codex", Workspace: "/w2"},
	}
	c := NewConsole(f)
	c.lastList = f.listSessions // 模拟 #l 后的编号语境
	return c, f
}

// gxMsgs 造 n 条可见消息（时间正序），文本含序号便于断言。
func gxMsgs(n int) []Message {
	msgs := make([]Message, 0, n)
	for i := 1; i <= n; i++ {
		kind := "user"
		if i%2 == 0 {
			kind = "agent"
		}
		msgs = append(msgs, Message{
			Kind: kind, ChatID: "c1", SessionTitle: "A",
			Text: fmt.Sprintf("消息%d", i),
		})
	}
	return msgs
}

// #gx 基本形态：按 lastList 编号取该会话最近 N 条可见消息（默认 5 条、
// 时间正序、滤噪声），快照进 lastRecent 且 digitRecent=true。
func TestHandleGX(t *testing.T) {
	t.Run("用法与非法参数", func(t *testing.T) {
		c, _ := gxConsole()
		for _, in := range []string{"#gx", "#gx abc", "#gx 1 0", "#gx 1 -2", "#gx 1 abc"} {
			if got := join(c.Handle(in)); got != c.text("usage_gx") {
				t.Fatalf("%q 应回用法, got %q", in, got)
			}
		}
	})

	t.Run("无效编号回 no_mapping", func(t *testing.T) {
		c, _ := gxConsole()
		if got := join(c.Handle("#gx 9")); got != c.text("no_mapping") {
			t.Fatalf("越界编号应回 no_mapping, got %q", got)
		}
	})

	t.Run("无 ChatID 的 ref 回 gx_none", func(t *testing.T) {
		c, _ := gxConsole()
		c.lastList = []SessionRef{{Num: 1, SessionID: "s1", Title: "磁盘会话"}}
		if got := join(c.Handle("#gx 1")); got != c.text("gx_none") {
			t.Fatalf("无 ChatID 应回 gx_none, got %q", got)
		}
	})

	t.Run("默认 5 条正序滤噪并快照", func(t *testing.T) {
		c, f := gxConsole()
		msgs := gxMsgs(7)
		// 追加一条噪声：最新消息是欢迎词，应被滤掉再截尾。
		msgs = append(msgs, Message{Kind: "agent", ChatID: "c1", Text: "hub.welcome|Noise"})
		f.sessMsgs = map[string][]Message{"c1": msgs}

		out := join(c.Handle("#gx 1"))
		// 5 条可见（消息3..7）：默认 5 条恰好覆盖全部，含序号 1..5。
		if !strings.Contains(out, "1. [用户][A] 消息3") {
			t.Fatalf("首条应是消息3（最近 5 条的最旧）: %q", out)
		}
		if !strings.Contains(out, "5. [用户][A] 消息7") {
			t.Fatalf("末条应是消息7: %q", out)
		}
		if strings.Contains(out, "hub.welcome") || strings.Contains(out, "消息2") {
			t.Fatalf("噪声与超出的旧消息不得出现: %q", out)
		}
		if !strings.Contains(out, "回复数字可查看该条全文") {
			t.Fatalf("应含 renderRecentList 的查全文引导: %q", out)
		}
		// 快照：取回的 msgs 进 lastRecent，且进入 #h 数字语境。
		if len(c.lastRecent) != 5 || !c.digitRecent {
			t.Fatalf("应快照 5 条进 lastRecent 且 digitRecent=true: %d %v",
				len(c.lastRecent), c.digitRecent)
		}
		got := join(c.Handle("5"))
		if !strings.Contains(got, "**消息 5**") || !strings.Contains(got, "消息7") {
			t.Fatalf("#gx 后纯数字 5 应查全文消息7: %q", got)
		}
	})

	t.Run("指定条数 3", func(t *testing.T) {
		c, f := gxConsole()
		f.sessMsgs = map[string][]Message{"c1": gxMsgs(5)}
		out := join(c.Handle("#gx 1 3"))
		if !strings.Contains(out, "1. [用户][A] 消息3") || strings.Contains(out, "4. [") {
			t.Fatalf("应取最近 3 条（消息3..5）: %q", out)
		}
		if len(c.lastRecent) != 3 {
			t.Fatalf("应快照 3 条: %d", len(c.lastRecent))
		}
	})

	t.Run("条数超上限钳 20", func(t *testing.T) {
		c, f := gxConsole()
		f.sessMsgs = map[string][]Message{"c1": gxMsgs(22)}
		out := join(c.Handle("#gx 1 99"))
		if !strings.Contains(out, "**最近 20 条消息:**") {
			t.Fatalf("条数 99 应钳 20 条: %q", out)
		}
		// 22 条滤噪后全可见，取最近 20 条 = 消息3..22。
		if !strings.Contains(out, "1. [用户][A] 消息3") || strings.Contains(out, "21. [") {
			t.Fatalf("应是最近 20 条（消息3..22）: %q", out)
		}
	})

	t.Run("全角数字归一", func(t *testing.T) {
		c, f := gxConsole()
		f.listSessions = []SessionRef{{Num: 1, ChatID: "c2", Title: "B", Tool: "codex", Workspace: "/w2"}}
		c.lastList = f.listSessions
		f.sessMsgs = map[string][]Message{"c2": gxMsgs(2)}
		out := join(c.Handle("#gx １ ２"))
		if !strings.Contains(out, "1. [用户][A] 消息1") || !strings.Contains(out, "2. [agent][A] 消息2") {
			t.Fatalf("全角编号/条数应归一后生效: %q", out)
		}
	})

	t.Run("该会话无消息回 gx_none", func(t *testing.T) {
		c, _ := gxConsole()
		if got := join(c.Handle("#gx 1")); got != c.text("gx_none") {
			t.Fatalf("空消息应回 gx_none, got %q", got)
		}
	})

	t.Run("经 Handle 端到端派发", func(t *testing.T) {
		// 端到端：#l 建映射后按编号 2 查 c2（编号 1 的 c1 有消息也不误取）。
		c, f := gxConsole()
		f.sessMsgs = map[string][]Message{"c1": gxMsgs(3), "c2": gxMsgs(2)}
		_ = c.Handle("#l")
		out := join(c.Handle("#gx 2"))
		if !strings.Contains(out, "**最近 2 条消息:**") || !strings.Contains(out, "消息1") {
			t.Fatalf("经 Handle 分发的 #gx 应按编号 2 查 c2: %q", out)
		}
		if strings.Contains(out, "消息3") {
			t.Fatalf("不得串到 c1 的消息: %q", out)
		}
	})
}
