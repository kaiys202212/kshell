package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
			res := map[string]any{"sessionId": "sess-new"}
			if os.Getenv("ACP_FAKE_MODELS") == "1" {
				res["models"] = map[string]any{
					"availableModels": []any{
						map[string]any{"modelId": "m1", "name": "Model One"},
						map[string]any{"modelId": "m2", "name": "Two", "description": "d2"},
					},
					"currentModelId": "m1",
				}
			}
			if os.Getenv("ACP_FAKE_CONFIGOPT") == "1" {
				res["configOptions"] = []any{map[string]any{
					"id": "model", "type": "select", "currentValue": "m1",
					"options": []any{map[string]any{"value": "m1", "name": "Model One"}},
				}}
			}
			reply(res)
		case "session/set_model":
			if os.Getenv("ACP_FAKE_NO_SETMODEL") == "1" {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(*m.ID), "error": map[string]any{"code": -32601, "message": "method not found"}})
			} else {
				reply(map[string]any{})
			}
		case "session/set_config_option":
			if os.Getenv("ACP_FAKE_NO_CONFIGOPT") == "1" {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(*m.ID), "error": map[string]any{"code": -32601, "message": "method not found"}})
			} else {
				reply(map[string]any{})
			}
		case "session/load":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-load",
				"update":    map[string]any{"sessionUpdate": "user_message_chunk", "messageId": "m0", "content": map[string]any{"type": "text", "text": "hello"}},
			}})
			reply(map[string]any{})
		case "session/prompt":
			if os.Getenv("ACP_FAKE_STDERR_EXIT") == "1" {
				// 模拟 agent 中途崩溃：先往 stderr 写原因再退出，退出码非 0。
				fmt.Fprintln(os.Stderr, "boom-reason: max turn requests exhausted")
				os.Exit(3)
			}
			if os.Getenv("ACP_FAKE_ECHO_PROMPT") == "1" {
				var p PromptParams
				_ = json.Unmarshal(m.Params, &p)
				reply(map[string]any{"stopReason": fmt.Sprintf("blocks:%d", len(p.Prompt))})
				continue
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-new",
				"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "m1", "content": map[string]any{"type": "text", "text": "working"}},
			}})
			// 反向请求用数字 id（对齐真实 agent，如 codebuddy 的 nextRequestId++）：
			// JSON-RPC 2.0 要求响应 id 与请求 id 值且类型一致，客户端按原始形态路由。
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "session/request_permission", "params": map[string]any{
				"sessionId": "sess-new",
				"toolCall":  map[string]any{"toolCallId": "c1", "title": "Write"},
				"options":   []any{map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"}},
			}})
			for sc.Scan() {
				// 严格按原始形态匹配数字 id：字符串 "99" 与数字 99 是不同的 key，
				// 应答若被字符串化，真实 agent 会静默丢弃 → session/prompt 永不返回。
				var rm struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(sc.Bytes(), &rm) != nil {
					continue
				}
				if strings.TrimSpace(string(rm.ID)) == "99" {
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
	return fakeAgentSpecEnv(t)
}

// fakeAgentSpecEnv 在标准假 agent 之上注入额外脚本开关（ACP_FAKE_* 环境变量）。
func fakeAgentSpecEnv(t *testing.T, extra ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append([]string{"ACP_FAKE_AGENT=1"}, extra...)
	return Spec{Path: exe, Args: []string{"-test.run=TestMain"}, Env: env}
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
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil || ss.SessionID != "sess-new" {
		t.Fatalf("new session: %v %+v", err, ss)
	}
	stop, err := a.Prompt(ctx, ss.SessionID, []ContentBlock{{Type: "text", Text: "do it"}})
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

func newTestAgent(t *testing.T, spec Spec) *Agent {
	t.Helper()
	a, err := NewAgent(context.Background(), spec, &captureHandler{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func testCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// session/new 响应里的 v1 models 字段必须解析到 SessionStart.Models。
func TestAgentNewSessionReturnsModels(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpecEnv(t, "ACP_FAKE_MODELS=1"))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if ss.Models == nil || len(ss.Models.AvailableModels) != 2 || ss.Models.CurrentModelID != "m1" {
		t.Fatalf("models = %+v", ss.Models)
	}
	if ss.Models.AvailableModels[1].ModelID != "m2" || ss.Models.AvailableModels[1].Description != "d2" {
		t.Fatalf("model[1] = %+v", ss.Models.AvailableModels[1])
	}
}

// configOptions 形态（id=model）必须识别为 ModelConfig。
func TestAgentNewSessionConfigOption(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpecEnv(t, "ACP_FAKE_CONFIGOPT=1"))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if ss.ModelConfig == nil || ss.ModelConfig.ID != "model" || len(ss.ModelConfig.Options) != 1 {
		t.Fatalf("ModelConfig = %+v", ss.ModelConfig)
	}
}

// agent 同时支持 set_model 时走主路径。
func TestAgentSetModelDirect(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpec(t))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetModel(ctx, ss.SessionID, "m2"); err != nil {
		t.Fatalf("set model: %v", err)
	}
}

// agent 不认识 set_model（-32601）时回退 session/set_config_option。
func TestAgentSetModelFallback(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpecEnv(t, "ACP_FAKE_NO_SETMODEL=1"))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetModel(ctx, ss.SessionID, "m2"); err != nil {
		t.Fatalf("set model fallback: %v", err)
	}
}

// 两条路都不支持时错误上抛，由 UI 提示。
func TestAgentSetModelUnsupported(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpecEnv(t, "ACP_FAKE_NO_SETMODEL=1", "ACP_FAKE_NO_CONFIGOPT=1"))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetModel(ctx, ss.SessionID, "m2"); err == nil {
		t.Fatal("expected error when both set paths unsupported")
	}
}

// Prompt 必须原样携带多形态 content blocks（用 echo 假 agent 回报块数验证）。
func TestAgentPromptSendsBlocks(t *testing.T) {
	a := newTestAgent(t, fakeAgentSpecEnv(t, "ACP_FAKE_ECHO_PROMPT=1"))
	ctx, cancel := testCtx()
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ss, err := a.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	blocks := []ContentBlock{
		{Type: "text", Text: "看这个"},
		{Type: "image", Data: "AAAA", MimeType: "image/png"},
	}
	stop, err := a.Prompt(ctx, ss.SessionID, blocks)
	if err != nil || stop != "blocks:2" {
		t.Fatalf("prompt: %v %q", err, stop)
	}
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

// agent 进程退出时必须把退出码与 stderr 尾部落到 Logf（宿主注入 desktop.log），
// 否则「agent 为什么停了」永远无据可查（Agent.Stderr() 此前无任何调用点）。
func TestAgentLogsExitCodeAndStderrTail(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	prev := Logf
	Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { Logf = prev })

	a, err := NewAgent(context.Background(), fakeAgentSpecEnv(t, "ACP_FAKE_STDERR_EXIT=1"), &captureHandler{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// 子进程写 stderr 后 exit 3：挂起的 Prompt 收到错误，Wait 返回退出码
	//（非零退出时 err 为 "exit status 3"，属预期）。
	if _, err := a.Prompt(ctx, "sess-new", []ContentBlock{{Type: "text", Text: "go"}}); err == nil {
		t.Fatal("expected error from crashed agent")
	}
	if code, err := a.Wait(); code != 3 || err == nil {
		t.Fatalf("Wait = (%d, %v), want (3, non-nil exit status)", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "code=3") {
		t.Fatalf("日志缺退出码, got:\n%s", joined)
	}
	if !strings.Contains(joined, "boom-reason") {
		t.Fatalf("日志缺 stderr 尾部, got:\n%s", joined)
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
	if _, err := a.LoadSession(ctx, "sess-load", "/tmp"); err != nil {
		t.Fatalf("load: %v", err)
	}
	// 通知在读循环内同步处理，LoadSession 返回时重放已就绪
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.updates) == 0 || !strings.HasPrefix(h.updates[0], "sess-load:user_message_chunk") {
		t.Fatalf("replay updates = %v", h.updates)
	}
}
