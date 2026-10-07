package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/applang"
)

// writeInboxFile 在收件箱目录落一个 payload 文件（模拟 notifier 写入）。
func writeInboxFile(t *testing.T, dir, name string, p agenthook.Payload) {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newNotifyTestApp 构造只带 Emit 的 App 并收集事件。
func newNotifyTestApp(t *testing.T) (*App, *[]string, *[]agenthook.Payload) {
	t.Helper()
	var mu sync.Mutex
	names := []string{}
	payloads := []agenthook.Payload{}
	a := NewAppWith(Options{Emit: func(name string, data ...any) {
		mu.Lock()
		defer mu.Unlock()
		names = append(names, name)
		if len(data) > 0 {
			if p, ok := data[0].(agenthook.Payload); ok {
				payloads = append(payloads, p)
			}
		}
	}})
	return a, &names, &payloads
}

// 合法 payload：文件被删除、事件按序分发。
func TestDispatchNotifyOnceReadsAndDeletes(t *testing.T) {
	dir := t.TempDir()
	p := agenthook.Payload{Tool: "codebuddy", Event: "Stop", TermKey: "session:s1", Workspace: "d:/ws", Summary: "done"}
	writeInboxFile(t, dir, "1.json", p)
	a, names, payloads := newNotifyTestApp(t)

	a.dispatchNotifyOnce(dir)

	if _, err := os.Stat(filepath.Join(dir, "1.json")); !os.IsNotExist(err) {
		t.Fatalf("payload 文件应被删除")
	}
	if len(*names) != 1 || (*names)[0] != "notify:agent" {
		t.Fatalf("应发出一次 notify:agent，得到 %v", *names)
	}
	if len(*payloads) != 1 || (*payloads)[0] != p {
		t.Fatalf("payload 应原样分发: %v", *payloads)
	}
}

// 坏 JSON：删除文件、跳过分发，不影响后续轮询。
func TestDispatchNotifyOnceDropsBadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, names, _ := newNotifyTestApp(t)

	a.dispatchNotifyOnce(dir)

	if len(*names) != 0 {
		t.Fatalf("坏文件不应分发: %v", *names)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("坏文件应被删除")
	}
}

// 缺 termKey 的 payload 无法归因到页签：删除并跳过。
func TestDispatchNotifyOnceDropsEmptyTermKey(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete"})
	a, names, _ := newNotifyTestApp(t)

	a.dispatchNotifyOnce(dir)

	if len(*names) != 0 {
		t.Fatalf("无 termKey 不应分发: %v", *names)
	}
	if _, err := os.Stat(filepath.Join(dir, "1.json")); !os.IsNotExist(err) {
		t.Fatalf("无 termKey 的文件应被删除")
	}
}

// 窗口隐藏（收进托盘）：额外弹 toast，标题/正文按映射生成。
func TestDispatchNotifyOnceHiddenShowsToast(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{
		Tool: "codebuddy", Event: "Stop", TermKey: "new:3", Workspace: `D:\ws`, Summary: "任务收尾完成",
	})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(true)
	t.Cleanup(func() { a.setWindowHidden(false) })

	origToast := notifyToast
	t.Cleanup(func() { notifyToast = origToast })
	var toastTitle, toastBody string
	toastCalls := 0
	notifyToast = func(title, body string) error {
		toastCalls++
		toastTitle, toastBody = title, body
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("隐藏时也应 emit: %v", *names)
	}
	if toastCalls != 1 {
		t.Fatalf("隐藏时应弹一次 toast，得到 %d", toastCalls)
	}
	if toastTitle != "CodeBuddy "+applang.T("toast.task_done") || toastBody != "任务收尾完成" {
		t.Fatalf("toast 文案不符: %q / %q", toastTitle, toastBody)
	}
}

// 窗口可见：只 emit，不弹 toast。
func TestDispatchNotifyOnceVisibleNoToast(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete", TermKey: "new:1"})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(false)

	origToast := notifyToast
	t.Cleanup(func() { notifyToast = origToast })
	toastCalls := 0
	notifyToast = func(title, body string) error {
		toastCalls++
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("可见时仍应 emit: %v", *names)
	}
	if toastCalls != 0 {
		t.Fatalf("可见时不应弹 toast，得到 %d", toastCalls)
	}
}

// payload → toast 标题/正文映射。
func TestNotifyToastText(t *testing.T) {
	cases := []struct {
		name      string
		p         agenthook.Payload
		wantTitle string
		wantBody  string
	}{
		{"codebuddy 完成", agenthook.Payload{Tool: "codebuddy", Event: "Stop", Summary: "done"}, "CodeBuddy " + applang.T("toast.task_done"), "done"},
		{"claude 等确认", agenthook.Payload{Tool: "claude", Event: "Notification", Summary: "需要权限"}, "Claude Code " + applang.T("toast.waiting_confirm"), "需要权限"},
		{"claude 出错", agenthook.Payload{Tool: "claude", Event: "error", Summary: "boom"}, "Claude Code " + applang.T("toast.task_error"), "boom"},
		{"gemini 等确认(attention)", agenthook.Payload{Tool: "gemini", Event: "attention", Summary: "等待输入"}, "Gemini " + applang.T("toast.waiting_confirm"), "等待输入"},
		{"codex 回合完成", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete"}, "Codex " + applang.T("toast.task_done"), ""},
		{"未知工具回退", agenthook.Payload{Tool: "mystery", Event: "Stop", Workspace: "d:/ws"}, "mystery " + applang.T("toast.task_done"), "d:/ws"},
		{"summary 优先于工作区", agenthook.Payload{Tool: "gemini", Workspace: "d:/ws", Summary: "ok"}, "Gemini " + applang.T("toast.task_done"), "ok"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			title, body := notifyToastText(c.p)
			if title != c.wantTitle || body != c.wantBody {
				t.Fatalf("得到 %q / %q，期望 %q / %q", title, body, c.wantTitle, c.wantBody)
			}
		})
	}
}
