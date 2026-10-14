package desktop

import (
	"context"
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

// 窗口隐藏（收进托盘）：弹独立气泡，不走系统 toast；标题/正文按映射生成。
func TestDispatchNotifyOnceHiddenShowsBubble(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{
		Tool: "codebuddy", Event: "Stop", TermKey: "new:3", Workspace: `D:\ws`, Summary: "任务收尾完成",
	})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(true)
	t.Cleanup(func() { a.setWindowHidden(false) })

	origBubble := notifyBubble
	origToast := notifyToast
	t.Cleanup(func() {
		notifyBubble = origBubble
		notifyToast = origToast
	})
	var bubbleTitle, bubbleBody, bubbleTermKey string
	bubbleCalls, toastCalls := 0, 0
	notifyBubble = func(title, body, termKey string) error {
		bubbleCalls++
		bubbleTitle, bubbleBody, bubbleTermKey = title, body, termKey
		return nil
	}
	notifyToast = func(title, body string) error {
		toastCalls++
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("隐藏时也应 emit: %v", *names)
	}
	if bubbleCalls != 1 {
		t.Fatalf("隐藏时应弹一次 bubble，得到 %d", bubbleCalls)
	}
	if toastCalls != 0 {
		t.Fatalf("隐藏时不应再弹系统 toast，得到 %d", toastCalls)
	}
	if bubbleTitle != "CodeBuddy "+applang.T("toast.task_done") || bubbleBody != "ws" || bubbleTermKey != "new:3" {
		t.Fatalf("bubble 参数不符: %q / %q / %q", bubbleTitle, bubbleBody, bubbleTermKey)
	}
}

// 窗口可见：只 emit，不弹 bubble / toast。
func TestDispatchNotifyOnceVisibleNoBubbleOrToast(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete", TermKey: "new:1"})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(false)

	origBubble := notifyBubble
	origToast := notifyToast
	t.Cleanup(func() {
		notifyBubble = origBubble
		notifyToast = origToast
	})
	bubbleCalls, toastCalls := 0, 0
	notifyBubble = func(title, body, termKey string) error {
		bubbleCalls++
		return nil
	}
	notifyToast = func(title, body string) error {
		toastCalls++
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("可见时仍应 emit: %v", *names)
	}
	if bubbleCalls != 0 {
		t.Fatalf("可见时不应弹 bubble，得到 %d", bubbleCalls)
	}
	if toastCalls != 0 {
		t.Fatalf("可见时不应弹 toast，得到 %d", toastCalls)
	}
}

// stubNotifyWindowState 注入已启动主窗的最小化/前台状态（ctx 非空才会走到这两项检查）。
func stubNotifyWindowState(t *testing.T, a *App, minimised, foreground bool) {
	t.Helper()
	a.mu.Lock()
	a.ctx = context.Background()
	a.mu.Unlock()
	origMin := windowIsMinimised
	origFg := windowIsForeground
	windowIsMinimised = func(context.Context) bool { return minimised }
	windowIsForeground = func() bool { return foreground }
	t.Cleanup(func() {
		windowIsMinimised = origMin
		windowIsForeground = origFg
	})
}

// 主窗未最小化但非前台（任务栏被挡住）：应弹独立气泡。
func TestDispatchNotifyOnceUnfocusedShowsBubble(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{
		Tool: "codex", Event: "agent-turn-complete", TermKey: "new:2", Summary: "后台完成",
	})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(false)
	stubNotifyWindowState(t, a, false, false)

	origBubble := notifyBubble
	origToast := notifyToast
	t.Cleanup(func() {
		notifyBubble = origBubble
		notifyToast = origToast
	})
	bubbleCalls, toastCalls := 0, 0
	notifyBubble = func(title, body, termKey string) error {
		bubbleCalls++
		return nil
	}
	notifyToast = func(title, body string) error {
		toastCalls++
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("非前台也应 emit: %v", *names)
	}
	if bubbleCalls != 1 {
		t.Fatalf("非前台应弹一次 bubble，得到 %d", bubbleCalls)
	}
	if toastCalls != 0 {
		t.Fatalf("非前台不应弹系统 toast，得到 %d", toastCalls)
	}
}

// 主窗前台：只 emit，不弹独立气泡。
func TestDispatchNotifyOnceFocusedNoBubble(t *testing.T) {
	dir := t.TempDir()
	writeInboxFile(t, dir, "1.json", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete", TermKey: "new:1"})
	a, names, _ := newNotifyTestApp(t)
	a.setWindowHidden(false)
	stubNotifyWindowState(t, a, false, true)

	origBubble := notifyBubble
	t.Cleanup(func() { notifyBubble = origBubble })
	bubbleCalls := 0
	notifyBubble = func(title, body, termKey string) error {
		bubbleCalls++
		return nil
	}

	a.dispatchNotifyOnce(dir)

	if len(*names) != 1 {
		t.Fatalf("前台仍应 emit: %v", *names)
	}
	if bubbleCalls != 0 {
		t.Fatalf("前台不应弹 bubble，得到 %d", bubbleCalls)
	}
}

// payload → toast 标题/正文映射。
// 正文固定为项目名（workspace 最后一段）：回复全文已进 AgentHub，
// 通知再重复铺开超长内容只会刷屏（2026-10-14 用户反馈）。
func TestNotifyToastText(t *testing.T) {
	cases := []struct {
		name      string
		p         agenthook.Payload
		wantTitle string
		wantBody  string
	}{
		{"codebuddy 完成", agenthook.Payload{Tool: "codebuddy", Event: "Stop", Workspace: `D:\a\proj`, Summary: "超长回复正文……"}, "CodeBuddy " + applang.T("toast.task_done"), "proj"},
		{"claude 等确认", agenthook.Payload{Tool: "claude", Event: "Notification", Workspace: `/home/u/proj`, Summary: "需要权限"}, "Claude Code " + applang.T("toast.waiting_confirm"), "proj"},
		{"claude 出错", agenthook.Payload{Tool: "claude", Event: "error", Workspace: `D:\ws`, Summary: "boom"}, "Claude Code " + applang.T("toast.task_error"), "ws"},
		{"gemini 等确认(attention)", agenthook.Payload{Tool: "gemini", Event: "attention", Workspace: `d:/ws`, Summary: "等待输入"}, "Gemini " + applang.T("toast.waiting_confirm"), "ws"},
		{"codex 无工作区正文为空", agenthook.Payload{Tool: "codex", Event: "agent-turn-complete", Summary: "x"}, "Codex " + applang.T("toast.task_done"), ""},
		{"未知工具回退", agenthook.Payload{Tool: "mystery", Event: "Stop", Workspace: "d:/ws"}, "mystery " + applang.T("toast.task_done"), "ws"},
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

// activateFromBubble 按窗口状态分支显示：任务栏最小化只走 SC_RESTORE 恢复
// （保留最大化状态），托盘隐藏 / 非前台被遮挡只前置（对齐托盘菜单）。
// 两者不可连调：无条件 Unminimise 会解除最大化，窗体尺寸被改。
func TestActivateFromBubbleWindowState(t *testing.T) {
	cases := []struct {
		name           string
		minimised      bool
		foreground     bool
		wantShow       int
		wantUnminimise int
	}{
		{"任务栏最小化：只 Unminimise", true, true, 0, 1},
		{"托盘隐藏：只 Show", false, false, 1, 0},
		{"非前台被遮挡：只 Show", false, false, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, names, _ := newNotifyTestApp(t)
			a.setWindowHidden(false)
			stubNotifyWindowState(t, a, c.minimised, c.foreground)
			origShow, origUnmin := windowShow, windowUnminimise
			showCalls, unminCalls := 0, 0
			windowShow = func(context.Context) { showCalls++ }
			windowUnminimise = func(context.Context) { unminCalls++ }
			t.Cleanup(func() {
				windowShow = origShow
				windowUnminimise = origUnmin
			})

			a.activateFromBubble("session:s1")

			if showCalls != c.wantShow || unminCalls != c.wantUnminimise {
				t.Fatalf("show=%d unminimise=%d, want %d/%d", showCalls, unminCalls, c.wantShow, c.wantUnminimise)
			}
			found := false
			for _, n := range *names {
				if n == "notify:focus" {
					found = true
				}
			}
			if !found {
				t.Fatalf("notify:focus 应照常发出，得到 %v", *names)
			}
		})
	}
}
