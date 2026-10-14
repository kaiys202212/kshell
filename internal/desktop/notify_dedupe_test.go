package desktop

import (
	"testing"
	"time"

	"github.com/yangk/kshell/internal/agenthook"
)

// TestNotifyDeduperSuppressesDuplicate 验证窗口内同 (termKey,event,summary) 只放行一次，
// 窗口过期后重新放行。
func TestNotifyDeduperSuppressesDuplicate(t *testing.T) {
	now := time.Unix(1000, 0)
	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWindow)
	d.now = func() time.Time { return now }
	p := agenthook.Payload{Tool: "claude", Event: "Notification", TermKey: "k1", Summary: "等待输入"}
	if !d.allow(p) {
		t.Fatal("首条应放行")
	}
	if d.allow(p) {
		t.Fatal("窗口内重复应被拦")
	}
	now = now.Add(notifyDedupWindow) // 恰好到窗界：已过期，放行
	if !d.allow(p) {
		t.Fatal("窗口过期后应放行")
	}
}

// TestNotifyDeduperKeyDistinction 验证不同摘要 / 事件 / 会话互不影响，不丢信息。
func TestNotifyDeduperKeyDistinction(t *testing.T) {
	now := time.Unix(1000, 0)
	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWindow)
	d.now = func() time.Time { return now }
	base := agenthook.Payload{Tool: "claude", Event: "Notification", TermKey: "k1", Summary: "等待输入"}
	if !d.allow(base) {
		t.Fatal("首条应放行")
	}
	other := base
	other.Summary = "权限请求"
	if !d.allow(other) {
		t.Fatal("不同摘要应放行")
	}
	other2 := base
	other2.Event = "Stop"
	if !d.allow(other2) {
		t.Fatal("不同事件应放行")
	}
	other3 := base
	other3.TermKey = "k2"
	if !d.allow(other3) {
		t.Fatal("不同会话应放行")
	}
	// 同 key 的空摘要同样参与去重（codex notify 常无 summary）
	empty := agenthook.Payload{Tool: "codex", Event: "agent-turn-complete", TermKey: "k3"}
	if !d.allow(empty) {
		t.Fatal("空摘要首条应放行")
	}
	if d.allow(empty) {
		t.Fatal("空摘要窗口内重复应被拦")
	}
}

// TestNotifyDeduperWindowByEvent 验证按事件分窗：等待确认类（Notification/attention）
// 用 30s 长窗拦住 claude 等待期间周期性重发的同一条通知；完成/出错类保持 2s 短窗，
// 避免同一会话 30s 内连续两轮完成（空 summary 时 key 相同）吞掉合法的第二条完成通知。
func TestNotifyDeduperWindowByEvent(t *testing.T) {
	now := time.Unix(1000, 0)
	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWaitWindow)
	d.now = func() time.Time { return now }

	wait := agenthook.Payload{Tool: "claude", Event: "Notification", TermKey: "k1", Summary: "Claude is waiting for your input"}
	if !d.allow(wait) {
		t.Fatal("首条等待确认应放行")
	}
	now = now.Add(5 * time.Second) // 已超短窗、仍在长窗内：这正是此前穿透导致重复的间隔
	if d.allow(wait) {
		t.Fatal("等待确认类长窗内重复应被拦")
	}

	done := agenthook.Payload{Tool: "claude", Event: "Stop", TermKey: "k1"}
	if !d.allow(done) {
		t.Fatal("完成类首条应放行")
	}
	now = now.Add(3 * time.Second) // 超过 2s 短窗
	if !d.allow(done) {
		t.Fatal("完成类超短窗后应放行，不得被长窗吞掉")
	}

	now = now.Add(notifyDedupWaitWindow) // 长窗也过期
	if !d.allow(wait) {
		t.Fatal("等待确认类长窗过期后应重新放行")
	}
}

// TestNotifyDeduperAttentionUsesWaitWindow 验证 gemini 的 attention 事件同样走长窗。
func TestNotifyDeduperAttentionUsesWaitWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWaitWindow)
	d.now = func() time.Time { return now }
	p := agenthook.Payload{Tool: "gemini", Event: "attention", TermKey: "k2", Summary: "等待输入"}
	if !d.allow(p) {
		t.Fatal("首条应放行")
	}
	now = now.Add(5 * time.Second)
	if d.allow(p) {
		t.Fatal("attention 长窗内重复应被拦")
	}
}

// TestDispatchNotifyOnceDedupes 端到端：同一 payload 两次入箱，
// 窗口内只 emit 一次、原生气泡也只弹一次。
func TestDispatchNotifyOnceDedupes(t *testing.T) {
	dir := t.TempDir()
	p := agenthook.Payload{Tool: "claude", Event: "Notification", TermKey: "session:s1", Workspace: `D:\ws`, Summary: "Claude is waiting for your input"}
	writeInboxFile(t, dir, "1.json", p)
	writeInboxFile(t, dir, "2.json", p)
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(true)
	t.Cleanup(func() { a.setWindowHidden(false) })

	origBubble, origToast := notifyBubble, notifyToast
	t.Cleanup(func() {
		notifyBubble = origBubble
		notifyToast = origToast
	})
	// 实例级去重器换可拨时钟的：既能隔离也能精确验证窗口行为（等待确认类窗也压成短窗，
	// 让本用例专注验证「窗口过期重新放行」这一行为本身）
	d := newNotifyDeduper(notifyDedupWindow, notifyDedupWindow)
	now := time.Unix(1000, 0)
	d.now = func() time.Time { return now }
	a.notifyDedup = d
	bubbleCalls, toastCalls := 0, 0
	notifyBubble = func(title, body, termKey string) error { bubbleCalls++; return nil }
	notifyToast = func(title, body string) error { toastCalls++; return nil }

	a.dispatchNotifyOnce(dir)
	a.dispatchNotifyOnce(dir)
	if len(*names) != 1 {
		t.Fatalf("窗口内重复 payload 应只 emit 一次，得到 %v", *names)
	}
	if bubbleCalls != 1 {
		t.Fatalf("原生气泡应只弹一次，得到 %d", bubbleCalls)
	}
	if toastCalls != 0 {
		t.Fatalf("不应弹系统 toast，得到 %d", toastCalls)
	}

	// 窗口过期后同一 payload 再次入箱：放行
	now = now.Add(notifyDedupWindow)
	writeInboxFile(t, dir, "3.json", p)
	a.dispatchNotifyOnce(dir)
	if len(*names) != 2 {
		t.Fatalf("窗口过期后应重新 emit，得到 %v", *names)
	}
	if bubbleCalls != 2 {
		t.Fatalf("窗口过期后应重新弹 bubble，得到 %d", bubbleCalls)
	}
}
