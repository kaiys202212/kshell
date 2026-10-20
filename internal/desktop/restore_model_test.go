package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
)

// OpenSessionACP 激活 Claude 会话后：JSONL 尾部上次助手模型在可用列表内且
// 与 Current 不同时，应自动 SetModel 恢复。
func TestOpenSessionACPRestoresLastModelFromJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"m2","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{
					{ModelID: "default", Name: "Default"},
					{ModelID: "m2", Name: "Two"},
				},
				CurrentModelID: "default",
			},
		},
		waitCh: make(chan struct{}),
	}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, &fakeChatCaptureBackend{conn: conn})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(conn.modelCalls) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(conn.modelCalls) != 1 || conn.modelCalls[0] != "m2" {
		t.Fatalf("应 SetModel(m2): %v", conn.modelCalls)
	}
}

// 上次模型不在可用列表：静默跳过，不 SetModel、不挡激活。
func TestOpenSessionACPSkipsRestoreWhenModelNotInList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	body := `{"type":"assistant","message":{"model":"gone-m","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := &fakeChatConn{
		start: acp.SessionStart{
			SessionID: "s1",
			Models: &acp.SessionModelState{
				AvailableModels: []acp.ModelInfo{{ModelID: "default", Name: "Default"}},
				CurrentModelID:  "default",
			},
		},
		waitCh: make(chan struct{}),
	}
	env := newChatTestApp(t, &providers.ACPDetection{
		Available: true, Source: "path", BinPath: "claude-agent-acp",
	}, &fakeChatCaptureBackend{conn: conn})
	env.app.mu.Lock()
	env.app.opts.Config.SessionMode = config.SessionModeACP
	env.app.result.Sessions[0].Path = path
	env.app.mu.Unlock()

	if _, err := env.app.OpenSessionACP("s1"); err != nil {
		t.Fatalf("OpenSessionACP: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(conn.modelCalls) != 0 {
		t.Fatalf("不在列表应不 SetModel: %v", conn.modelCalls)
	}
}
