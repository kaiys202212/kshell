package agenthook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHome 把 HOME/USERPROFILE 都指向临时目录，让 inbox 落到测试可控的位置。
// os.UserHomeDir 在 Windows 读 USERPROFILE、Unix 读 HOME，两个都设保证跨平台可移植。
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func inboxPath(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(home, ".kshell", "notify", "inbox")
}

// readInboxFiles 返回 inbox 下全部 *.json 文件的内容；目录不存在返回空。
func readInboxFiles(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(inboxPath(t, home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("读取 inbox 失败: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(inboxPath(t, home), e.Name()))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		out = append(out, string(b))
	}
	return out
}

func mustOnePayload(t *testing.T, home string) Payload {
	t.Helper()
	files := readInboxFiles(t, home)
	if len(files) != 1 {
		t.Fatalf("inbox 应恰好有 1 个通知文件，实际 %d 个", len(files))
	}
	var p Payload
	if err := json.Unmarshal([]byte(files[0]), &p); err != nil {
		t.Fatalf("通知文件不是合法 payload JSON: %v\n内容: %s", err, files[0])
	}
	return p
}

func TestHandleArgvJSON(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-1")
	t.Setenv("KSHELL_WORKSPACE", "d:/ws")

	// codex notify 形态：JSON 作为最后一个 argv，前面带 tool 名
	code := Handle([]string{"agent-hook", "codex", `{"type":"agent-turn-complete","turn-id":"t1"}`})
	if code != 0 {
		t.Fatalf("Handle 返回 %d，想要 0", code)
	}

	p := mustOnePayload(t, home)
	if p.Tool != "codex" {
		t.Errorf("Tool = %q, 想要 codex", p.Tool)
	}
	if p.Event != "agent-turn-complete" {
		t.Errorf("Event = %q, 想要 agent-turn-complete", p.Event)
	}
	if p.TermKey != "term-1" {
		t.Errorf("TermKey = %q, 想要 term-1", p.TermKey)
	}
	if p.Workspace != "d:/ws" {
		t.Errorf("Workspace = %q, 想要 d:/ws", p.Workspace)
	}
	if !strings.Contains(p.Raw, "agent-turn-complete") {
		t.Errorf("Raw 应保留原始 JSON, 实际 %q", p.Raw)
	}
	if p.Ts <= 0 {
		t.Errorf("Ts 应为正数 UnixNano, 实际 %d", p.Ts)
	}

	// 原子写：不允许残留 .tmp 临时文件
	entries, err := os.ReadDir(inboxPath(t, home))
	if err != nil {
		t.Fatalf("读取 inbox 失败: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("残留临时文件 %s", e.Name())
		}
	}
}

func TestHandleArgvJSONWithoutTool(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-1")

	// 约定形态：第二个参数本身就是 JSON（无 tool 名）
	code := Handle([]string{"agent-hook", `{"hook_event_name":"Stop"}`})
	if code != 0 {
		t.Fatalf("Handle 返回 %d，想要 0", code)
	}

	p := mustOnePayload(t, home)
	if p.Tool != "" {
		t.Errorf("Tool = %q, 想要空", p.Tool)
	}
	if p.Event != "Stop" {
		t.Errorf("Event = %q, 想要 Stop", p.Event)
	}
}

func TestHandleStdinJSON(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-2")
	t.Setenv("KSHELL_WORKSPACE", "")

	// claude/codebuddy/cursor 形态：tool 在 argv，JSON 从 stdin 读（带尾部换行）
	code := handle([]string{"agent-hook", "claude"},
		strings.NewReader("{\"hook_event_name\":\"Notification\",\"message\":\"任务完成\"}\n"))
	if code != 0 {
		t.Fatalf("handle 返回 %d，想要 0", code)
	}

	p := mustOnePayload(t, home)
	if p.Tool != "claude" {
		t.Errorf("Tool = %q, 想要 claude", p.Tool)
	}
	if p.Event != "Notification" {
		t.Errorf("Event = %q, 想要 Notification", p.Event)
	}
	if p.Summary != "任务完成" {
		t.Errorf("Summary = %q, 想要 任务完成", p.Summary)
	}
	if p.Workspace != "" {
		t.Errorf("Workspace = %q, 想要空", p.Workspace)
	}
}

func TestHandleNoTermKeyFiltered(t *testing.T) {
	home := fakeHome(t)
	// 空值视为未注入：只响应 kshell 启动的会话
	t.Setenv("KSHELL_TERM_KEY", "")

	code := handle([]string{"agent-hook", "claude"},
		strings.NewReader(`{"hook_event_name":"Stop"}`))
	if code != 0 {
		t.Fatalf("handle 返回 %d，想要 0", code)
	}
	if files := readInboxFiles(t, home); len(files) != 0 {
		t.Fatalf("未注入 termKey 时不应落盘，实际 %d 个文件", len(files))
	}
}

func TestHandleBadJSONSilent(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-3")

	code := handle([]string{"agent-hook", "claude"}, strings.NewReader(`{oops`))
	if code != 0 {
		t.Fatalf("坏 JSON 也必须返回 0, 实际 %d", code)
	}
	if files := readInboxFiles(t, home); len(files) != 0 {
		t.Fatalf("坏 JSON 不应落盘，实际 %d 个文件", len(files))
	}
}

func TestHandleEmptyStdinSilent(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-3")

	code := handle([]string{"agent-hook", "claude"}, strings.NewReader("  \n"))
	if code != 0 {
		t.Fatalf("空输入也必须返回 0, 实际 %d", code)
	}
	if files := readInboxFiles(t, home); len(files) != 0 {
		t.Fatalf("空输入不应落盘，实际 %d 个文件", len(files))
	}
}

func TestHandleMissingEventFields(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("KSHELL_TERM_KEY", "term-4")

	// 提取不到 event/summary 就留空，不报错、照常落盘
	code := handle([]string{"agent-hook", "gemini"}, strings.NewReader(`{"foo":"bar"}`))
	if code != 0 {
		t.Fatalf("handle 返回 %d，想要 0", code)
	}

	p := mustOnePayload(t, home)
	if p.Event != "" {
		t.Errorf("Event = %q, 想要空", p.Event)
	}
	if p.Summary != "" {
		t.Errorf("Summary = %q, 想要空", p.Summary)
	}
	if p.Raw != `{"foo":"bar"}` {
		t.Errorf("Raw = %q, 想要原始 JSON", p.Raw)
	}
}

func TestCleanupInboxRemovesOldKeepsNew(t *testing.T) {
	home := fakeHome(t)
	dir := inboxPath(t, home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("建 inbox 失败: %v", err)
	}

	oldFile := filepath.Join(dir, "100-old.json")
	if err := os.WriteFile(oldFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("写旧文件失败: %v", err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, past, past); err != nil {
		t.Fatalf("改 mtime 失败: %v", err)
	}
	newFile := filepath.Join(dir, "200-new.json")
	if err := os.WriteFile(newFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("写新文件失败: %v", err)
	}
	// 非 .json 后缀不清理
	keepTxt := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keepTxt, []byte("x"), 0o600); err != nil {
		t.Fatalf("写 txt 失败: %v", err)
	}
	if err := os.Chtimes(keepTxt, past, past); err != nil {
		t.Fatalf("改 mtime 失败: %v", err)
	}

	CleanupInbox(time.Hour)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("超龄 json 应被删除, stat err = %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("新鲜 json 应保留, stat err = %v", err)
	}
	if _, err := os.Stat(keepTxt); err != nil {
		t.Errorf("非 json 文件不应被清理, stat err = %v", err)
	}
}

func TestCleanupInboxMissingDirSilent(t *testing.T) {
	home := fakeHome(t)
	// 目录不存在：静默返回，不 panic、也不顺手建目录
	CleanupInbox(time.Hour)
	if _, err := os.Stat(inboxPath(t, home)); !os.IsNotExist(err) {
		t.Errorf("Cleanup 不应创建 inbox 目录, stat err = %v", err)
	}
}
