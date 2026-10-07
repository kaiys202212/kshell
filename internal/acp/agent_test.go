package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAgentMain 是被测 Agent 拉起的假 agent：按行读 JSON-RPC 并脚本化应答。
func fakeAgentMain() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" {
			continue
		}
		reply := func(result any) {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(*m.ID), "result": result})
		}
		switch m.Method {
		case "initialize":
			reply(map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
				"agentInfo":         map[string]any{"name": "fake"},
			})
		case "session/new":
			reply(map[string]any{"sessionId": "sess-new"})
		case "session/load":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-load",
				"update":    map[string]any{"sessionUpdate": "user_message_chunk", "messageId": "m0", "content": map[string]any{"type": "text", "text": "hello"}},
			}})
			reply(map[string]any{})
		case "session/prompt":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-new",
				"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "m1", "content": map[string]any{"type": "text", "text": "working"}},
			}})
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "session/request_permission", "params": map[string]any{
				"sessionId": "sess-new",
				"toolCall":  map[string]any{"toolCallId": "c1", "title": "Write"},
				"options":   []any{map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"}},
			}})
			for sc.Scan() {
				var rm message
				if json.Unmarshal(sc.Bytes(), &rm) != nil {
					continue
				}
				if rm.ID != nil && *rm.ID == "99" {
					break
				}
			}
			reply(map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
			// 通知无响应
		case "test/exit":
			os.Exit(0)
		}
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("ACP_FAKE_AGENT") == "1" {
		fakeAgentMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type captureHandler struct {
	mu       sync.Mutex
	updates  []string
	permDone chan PermissionOutcome
}

func (h *captureHandler) SessionUpdate(sessionID string, update json.RawMessage) {
	var head struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	_ = json.Unmarshal(update, &head)
	h.mu.Lock()
	h.updates = append(h.updates, sessionID+":"+head.SessionUpdate)
	h.mu.Unlock()
}

func (h *captureHandler) RequestPermission(ctx context.Context, sessionID, requestID string, p RequestPermissionParams) (PermissionOutcome, error) {
	out := PermissionOutcome{Outcome: "selected", OptionID: p.Options[0].OptionID}
	if h.permDone != nil {
		h.permDone <- out
	}
	return out, nil
}

func fakeAgentSpec(t *testing.T) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Path: exe, Args: []string{"-test.run=TestMain"}, Env: []string{"ACP_FAKE_AGENT=1"}}
}

// ACP 错误一律以 wire key 暴露给前端翻译（参数以 | 分隔），不再是中文字面量。
func TestAgentErrorsWireKeys(t *testing.T) {
	if _, err := NewAgent(context.Background(), Spec{}, nil); err == nil || err.Error() != "err.acp.no_agent" {
		t.Fatalf("no_agent = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "nonexistent-acp-binary")
	if _, err := NewAgent(context.Background(), Spec{Path: missing}, nil); err == nil || !strings.HasPrefix(err.Error(), "err.acp.start_failed|") {
		t.Fatalf("start_failed = %v", err)
	}
	a, err := NewAgent(context.Background(), fakeAgentSpec(t), &captureHandler{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	_ = a.Close()
	if err := a.call(context.Background(), "ping", nil, nil); err == nil || err.Error() != "err.acp.closed" {
		t.Fatalf("closed = %v", err)
	}
}

func TestAgentHandshakeAndPrompt(t *testing.T) {
	h := &captureHandler{permDone: make(chan PermissionOutcome, 1)}
	a, err := NewAgent(context.Background(), fakeAgentSpec(t), h)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	init, err := a.Initialize(ctx)
	if err != nil || !init.AgentCapabilities.LoadSession {
		t.Fatalf("initialize: %v %+v", err, init)
	}
	sid, err := a.NewSession(ctx, "/tmp")
	if err != nil || sid != "sess-new" {
		t.Fatalf("new session: %v %q", err, sid)
	}
	stop, err := a.Prompt(ctx, sid, "do it")
	if err != nil || stop != "end_turn" {
		t.Fatalf("prompt: %v %q", err, stop)
	}
	select {
	case out := <-h.permDone:
		if out.Outcome != "selected" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission handler not called")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !containsUpdate(h.updates, "sess-new:agent_message_chunk") {
		t.Fatalf("expected sess-new:agent_message_chunk update, got %v", h.updates)
	}
}

func containsUpdate(updates []string, want string) bool {
	for _, u := range updates {
		if u == want {
			return true
		}
	}
	return false
}

func TestAgentProcessExitFailsPendingCall(t *testing.T) {
	a, err := NewAgent(context.Background(), fakeAgentSpec(t), &captureHandler{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// test/exit 让子进程在请求挂起时退出：等待者必须收到显式错误而非零值成功。
	if err := a.call(ctx, "test/exit", nil, nil); err == nil {
		t.Fatal("expected error when process exits during pending call")
	}
	// Wait 必须能正常返回，不能挂起。
	waitDone := make(chan struct{})
	go func() { _, _ = a.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait hung after process exit")
	}
}

func TestAgentLoadSessionReplays(t *testing.T) {
	h := &captureHandler{}
	a, err := NewAgent(context.Background(), fakeAgentSpec(t), h)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession(ctx, "sess-load", "/tmp"); err != nil {
		t.Fatalf("load: %v", err)
	}
	// 通知在读循环内同步处理，LoadSession 返回时重放已就绪
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.updates) == 0 || !strings.HasPrefix(h.updates[0], "sess-load:user_message_chunk") {
		t.Fatalf("replay updates = %v", h.updates)
	}
}
