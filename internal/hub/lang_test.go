package hub

import (
	"strings"
	"testing"
)

// 英文态：NewConsoleLang(ex, "en") 下帮助全文应含 "Commands"。
func TestHelpEnglish(t *testing.T) {
	c := NewConsoleLang(&fakeExecutor{hasActivity: true,
		lastActivityRef: wsRef(0, "c1", "", `D:\work\demo`, "claude", "fix login", true),
	}, "en")
	out := join(c.Handle("#?"))
	if !strings.Contains(out, "Commands") {
		t.Fatalf("英文态帮助应含 Commands: %s", out)
	}
	if strings.Contains(out, "命令列表") {
		t.Fatalf("英文态不应出现中文文案: %s", out)
	}
}

// 英文态常用命令抽查：无会话 / 未知命令 / 编号不存在。
func TestCommonTextsEnglish(t *testing.T) {
	c := NewConsoleLang(&fakeExecutor{}, "en")
	if got := join(c.Handle("#l")); got != "No sessions yet" {
		t.Fatalf("英文态 #l = %q", got)
	}
	if got := join(c.Handle("#x")); !strings.Contains(got, "Unknown command") {
		t.Fatalf("英文态未知命令 = %q", got)
	}
	if got := join(c.Handle("5")); !strings.Contains(got, "No such number") {
		t.Fatalf("英文态编号不存在 = %q", got)
	}
	if got := join(c.Handle("hello")); !strings.Contains(got, "No exact session matched") || !strings.Contains(got, "1. new session") {
		t.Fatalf("英文态无会话应进仅新建入口待选 = %q", got)
	}
}

// 中文态（NewConsole 默认）保持既有中文文案：既有测试断言不回归。
func TestTextDefaultsChinese(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if got := join(c.Handle("#l")); got != "暂无会话" {
		t.Fatalf("默认中文态 #l = %q", got)
	}
}
