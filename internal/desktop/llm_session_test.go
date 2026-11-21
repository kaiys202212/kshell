package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// TestLLMSessionIDPersistedAndStable 验证应用级 LLM 会话 ID：首次生成非空、
// 原子落盘 state 目录、同实例/跨实例调用稳定沿用（opencode GO 网关要求
// 稳定会话头，重启漂移会让网关侧路由/缓存归属失效）。
func TestLLMSessionIDPersistedAndStable(t *testing.T) {
	root := t.TempDir()
	app := &App{opts: Options{Layout: config.Layout{State: root}}}

	first := app.llmSessionID()
	if first == "" {
		t.Fatal("首次应生成非空 session ID")
	}
	if !strings.Contains(first, "-") || len(first) != 36 {
		t.Fatalf("应为标准 UUID 形态, got %q", first)
	}
	b, err := os.ReadFile(filepath.Join(root, "llm-session.json"))
	if err != nil {
		t.Fatalf("state 文件应存在: %v", err)
	}
	if !strings.Contains(string(b), first) {
		t.Fatalf("文件应记录该 ID: %s", b)
	}
	if again := app.llmSessionID(); again != first {
		t.Fatalf("同实例应稳定: %q != %q", again, first)
	}
	// 新实例（模拟重启）读同一 state 文件应沿用
	app2 := &App{opts: Options{Layout: config.Layout{State: root}}}
	if got := app2.llmSessionID(); got != first {
		t.Fatalf("跨实例应沿用落盘值: %q != %q", got, first)
	}
}

// TestLLMSessionIDCorruptFileRegenerates：state 文件损坏时不得返回空或报错，
// 应重新生成并覆盖写回（与 config 损坏备份重建同思路，但这是辅助状态，直接重建）。
func TestLLMSessionIDCorruptFileRegenerates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "llm-session.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatalf("预置损坏文件: %v", err)
	}
	app := &App{opts: Options{Layout: config.Layout{State: root}}}
	got := app.llmSessionID()
	if got == "" {
		t.Fatal("损坏文件时应重新生成非空 ID")
	}
	b, err := os.ReadFile(filepath.Join(root, "llm-session.json"))
	if err != nil || !strings.Contains(string(b), got) {
		t.Fatalf("损坏后应重写落盘: err=%v body=%s", err, b)
	}
}

// TestLLMSessionIDNoStateDirEphemeral：Layout.State 未装配（测试零值 Layout）
// 时退化为进程内临时 ID——非空、不落盘、进程内稳定。
func TestLLMSessionIDNoStateDirEphemeral(t *testing.T) {
	app := &App{}
	first := app.llmSessionID()
	if first == "" {
		t.Fatal("无 state 目录也应返回非空临时 ID")
	}
	if again := app.llmSessionID(); again != first {
		t.Fatalf("进程内应稳定: %q != %q", again, first)
	}
}
