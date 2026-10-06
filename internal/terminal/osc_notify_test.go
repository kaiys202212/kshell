package terminal

import (
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agenthook"
)

// timeSleepShort 短暂等待：给「不应发生的事件」留出暴露窗口。
func timeSleepShort() { time.Sleep(50 * time.Millisecond) }

// collectNotify 是线程安全的 onNotify 命中收集器。
type collectNotify struct {
	mu   sync.Mutex
	got  []agenthook.Payload
	fail bool // 置 true 表示未注册回调也应无 panic（用 nil fn 模拟）
}

func (c *collectNotify) add(p agenthook.Payload) {
	c.mu.Lock()
	c.got = append(c.got, p)
	c.mu.Unlock()
}

func (c *collectNotify) all() []agenthook.Payload {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agenthook.Payload(nil), c.got...)
}

// geminiInfo 是 gemini 会话的终端 Info。
func geminiInfo() Info {
	return Info{Kind: KindSession, SessionID: "g1", Workspace: "D:/ws", Title: "gemini 会话", ToolID: "gemini"}
}

// TestManagerEmitsOSCNotifyForGemini 验证 gemini 会话输出中的 OSC 9/777
// 经 onNotify 回调发出，payload 归因到会话 key / 工作区。
func TestManagerEmitsOSCNotifyForGemini(t *testing.T) {
	m, b, _ := newTestManager(t)
	c := &collectNotify{}
	m.SetOnNotify(c.add)

	if _, err := m.Open("session:g1", geminiInfo(), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	b.lastHandle().feed("thinking...\x1b]9;等待输入\x07done")
	waitFor(t, "OSC 通知回调", func() bool { return len(c.all()) == 1 })

	p := c.all()[0]
	if p.Tool != "gemini" || p.Event != "attention" {
		t.Fatalf("Tool/Event = %q/%q", p.Tool, p.Event)
	}
	if p.TermKey != "session:g1" || p.Workspace != "D:/ws" {
		t.Fatalf("TermKey/Workspace = %q/%q", p.TermKey, p.Workspace)
	}
	if p.Summary != "等待输入" || p.Raw != "\x1b]9;等待输入\x07" {
		t.Fatalf("Summary/Raw = %q/%q", p.Summary, p.Raw)
	}
	if p.Ts == 0 {
		t.Fatal("Ts 应为写入时刻")
	}

	// OSC 9;4 进度条不算通知
	before := len(c.all())
	b.lastHandle().feed("\x1b]9;4;running;50\x07")
	b.lastHandle().feed("\x1b]777;notify;标题;正文\x07")
	waitFor(t, "第二次通知回调", func() bool { return len(c.all()) == before+1 })
	if got := c.all()[before]; got.Summary != "标题" {
		t.Fatalf("777 通知 Summary = %q", got.Summary)
	}

	// 普通输出不触发
	before = len(c.all())
	b.lastHandle().feed("plain output \x1b[2J")
	timeSleepShort()
	if got := len(c.all()); got != before {
		t.Fatalf("普通输出不应触发通知, got %d", got)
	}
}

// TestManagerOSCNotifySkipsNonGemini 验证非 gemini 会话不产生 OSC 通知
//（claude 走 hooks 通道，其它工具无此约定）。
func TestManagerOSCNotifySkipsNonGemini(t *testing.T) {
	m, b, _ := newTestManager(t)
	c := &collectNotify{}
	m.SetOnNotify(c.add)

	if _, err := m.Open("session:c1", openInfo("c1"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	b.lastHandle().feed("\x1b]9;不应通知\x07")
	timeSleepShort()
	if got := c.all(); len(got) != 0 {
		t.Fatalf("非 gemini 会话不应触发通知: %+v", got)
	}
}

// TestManagerOSCNotifyUnsetCallback 验证未注册回调时命中不 panic。
func TestManagerOSCNotifyUnsetCallback(t *testing.T) {
	m, b, r := newTestManager(t)

	if _, err := m.Open("session:g1", geminiInfo(), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	b.lastHandle().feed("\x1b]9;无人监听\x07")
	// 数据仍应原样投递给 onData（只观察不消费）
	waitFor(t, "输出投递", func() bool { return r.output() == "\x1b]9;无人监听\x07" })
}

// TestManagerOSCNotifyAfterRestart 验证重启后扫描器状态重置：
// 跨块残留的半个序列不会与新进程的输出拼出错误命中。
func TestManagerOSCNotifyAfterRestart(t *testing.T) {
	m, b, _ := newTestManager(t)
	c := &collectNotify{}
	m.SetOnNotify(c.add)

	if _, err := m.Open("session:g1", geminiInfo(), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	// 喂半个无终止符序列后让进程退出重启
	b.lastHandle().feed("\x1b]9;半截")
	b.lastHandle().finish(0)
	waitFor(t, "退出回调", func() bool { return len(m.List()) == 1 && m.List()[0].Status == StatusExited })

	if _, err := m.Open("session:g1", geminiInfo(), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("重启 Open error: %v", err)
	}
	b.lastHandle().feed("尾巴\x1b]9;正常\x07")
	waitFor(t, "重启后通知", func() bool { return len(c.all()) == 1 })
	if got := c.all()[0]; got.Summary != "正常" {
		t.Fatalf("重启后应只识别新序列, got %+v", got)
	}
}
