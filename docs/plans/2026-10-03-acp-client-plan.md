# 通用 ACP 客户端框架 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 kshell 桌面端以 ACP 协议驱动 Claude Code（原生聊天 UI），并把协议层做成通用 ACP 客户端框架。

**Architecture:** 新增 `internal/acp`（stdio JSON-RPC + ACP 类型）、`internal/chat`（进程与会话编排，Backend 可打桩）；providers/discovery/launch 增加可选 ACP 能力与适配器探测；desktop 增加 `chat.go` 绑定与统一打开入口（chat/terminal 自动回退）；前端新增聊天页签类型与 `ChatView`，复用现有 store 与事件总线模式。

**Tech Stack:** Go 1.23+（无 ACP 官方 SDK，自实现 JSON-RPC）、Wails v2、React 19 + Zustand + vitest、`react-markdown` + `remark-gfm`。设计依据见 `docs/plans/2026-10-03-acp-client-design.md`。

**范围提醒**：设计 §2「非目标」一律不做；本计划 15 个任务对应设计里程碑 M1–M6。

---

## 文件结构

**新建（Go）**

- `internal/acp/rpc.go` — ndjson JSON-RPC 编解码与 message 结构
- `internal/acp/rpc_test.go`
- `internal/acp/protocol.go` — ACP 参数/结果类型、Handler 接口、Spec
- `internal/acp/agent.go` — 子进程 + 请求/通知/反向请求读循环
- `internal/acp/agent_test.go`
- `internal/chat/types.go` — Spec/Info/Update/ToolCall/PermissionRequest 等
- `internal/chat/manager.go` — 编排 Manager
- `internal/chat/manager_test.go`
- `internal/chat/backend.go` — 真实 Backend（包 `acp.Agent` 实现 Conn）
- `internal/providers/acp.go` — ACPAdapter / ACPProvider / DetectACP
- `internal/providers/acp_test.go`
- `internal/desktop/chat.go` — 绑定方法 + 事件
- `internal/desktop/chat_test.go`

**修改（Go）**

- `internal/providers/claude.go` — 实现 `ACPAdapter()`
- `internal/discovery/tools.go` — `Tool.ACP` + `DetectAll` 填充
- `internal/launch/launch.go` — `ForSessionACP` / `ForWorkspaceACP`
- `internal/desktop/app.go` — `Options.Chats`、`initRealDeps`、`Shutdown`、`OpenSession`/`OpenWorkspace`（放 `chat.go`）

**新建（前端）**

- `frontend/src/components/ChatView.tsx` + `ChatView.test.tsx`
- `frontend/src/state/chatUpdate.ts` — `applyChatUpdate` 纯函数 + `chatUpdate.test.ts`

**修改（前端）**

- `frontend/src/lib/api.ts` — 类型、绑定封装、事件订阅
- `frontend/src/state/store.ts` — chats/chatItems/chatPermissions
- `frontend/src/pages/WorkspaceTab.tsx` — 聊天页签
- `frontend/src/App.tsx` — 事件统一订阅 + 关闭连带 + 重建镜像
- `frontend/package.json` — 新依赖
- `docs/smoke-test*.md` — 冒烟条目

**约定**：Go 结构体不带 json tag 时 Wails/JSON 输出 PascalCase；前端类型沿用 PascalCase（与 `TerminalInfo`/`Workspace` 一致）。`internal/acp` 内部协议结构带小写 json tag（按 ACP 规范）。

---

## M1：协议层 internal/acp

### Task 1: JSON-RPC 编解码（rpc.go）

**Files:**
- Create: `internal/acp/rpc.go`
- Test: `internal/acp/rpc_test.go`

- [ ] **Step 1: 写失败测试**

```go
package acp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c := newCodec(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`+"\n"), &buf)

	m, err := c.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.JSONRPC != "2.0" || m.Method != "initialize" || m.ID == nil || *m.ID != "1" {
		t.Fatalf("unexpected message: %+v", m)
	}
	var p struct{ ProtocolVersion int `json:"protocolVersion"` }
	if err := json.Unmarshal(m.Params, &p); err != nil || p.ProtocolVersion != 1 {
		t.Fatalf("params decode: %v %+v", err, p)
	}

	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]any{}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 写出的消息必须是单行、以 \n 结尾
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("want single newline-terminated line, got %q", out)
	}
	if strings.Contains(strings.TrimSuffix(out, "\n"), "\n") {
		t.Fatalf("embedded newline: %q", out)
	}
}

func TestCodecStreamsAcrossReads(t *testing.T) {
	// 分片读入（每次 1 字节）也要能正确解出一条消息
	r := iotest_oneByte(`{"jsonrpc":"2.0","method":"session/update","params":{}}` + "\n")
	c := newCodec(r, io.Discard)
	if _, err := c.read(); err != nil {
		t.Fatalf("read across chunks: %v", err)
	}
}

func TestCodecBadJSON_SurfacesError(t *testing.T) {
	c := newCodec(strings.NewReader("not json\n"), io.Discard)
	if _, err := c.read(); err == nil {
		t.Fatal("want decode error")
	}
}
```

在文件底部加测试辅助（避免额外 import 遗漏）：

```go
func iotest_oneByte(s string) io.Reader { return iotest.OneByteReader(strings.NewReader(s)) }
```

（import 增加 `"io"`、`"testing/iotest"`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/acp/ -run TestCodec -v`
Expected: 编译失败（`newCodec` / `codec` 未定义）。

- [ ] **Step 3: 实现 rpc.go**

```go
// Package acp 实现 Agent Client Protocol v1 的客户端侧：
// stdio 上换行分隔的 JSON-RPC 2.0 编解码与 agent 子进程交互。
package acp

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const jsonrpcVersion = "2.0"

// RPCError 是 JSON-RPC 错误对象。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp rpc error %d: %s", e.Code, e.Message) }

// message 是 JSON-RPC 请求/响应/通知的统一解包结构。
// 判定：Method!="" && ID!=nil → 反向请求；Method!="" && ID==nil → 通知；Method=="" && ID!=nil → 响应。
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *string          `json:"id,omitempty"` // 保留原始字符串形态，比较无需关心数字/字符串
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// codec 在读取端用 json.Decoder（天然按 JSON 值流式切分，兼容分片读）；
// 写入端串行化 marshaled JSON + '\n'。写锁由调用方（Agent.writeMu）保证。
type codec struct {
	dec *json.Decoder
	w   io.Writer
}

func newCodec(r io.Reader, w io.Writer) *codec {
	return &codec{dec: json.NewDecoder(r), w: w}
}

func (c *codec) read() (message, error) {
	var m message
	if err := c.dec.Decode(&m); err != nil {
		return message{}, err
	}
	return m, nil
}

func (c *codec) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// requestEnvelope / responseEnvelope / notificationEnvelope 是写出的三种消息。
type requestEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Result  any         `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type notificationEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
```

去掉未用的 `sync` import（实现里不需要）；保留 `fmt`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/acp/ -run TestCodec -v`
Expected: PASS。

- [ ] **Step 5: 提交**

当前分支 `feat/acp-chat`（worktree 内）。用户未要求时**不提交**——本计划所有「提交」步骤仅在用户明确要求后执行；否则跳过。

---

### Task 2: ACP 协议类型（protocol.go）

**Files:**
- Create: `internal/acp/protocol.go`
- Test: `internal/acp/protocol_test.go`

- [ ] **Step 1: 写失败测试（解码 session/update 信封与权限参数）**

```go
package acp

import (
	"encoding/json"
	"testing"
)

func TestDecodeUpdateEnvelope(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"hi"}}}`)
	sid, upd, err := decodeUpdateParams(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sid != "s1" {
		t.Fatalf("sessionId = %q", sid)
	}
	var head struct {
		SessionUpdate string `json:"sessionUpdate"`
		MessageID     string `json:"messageId"`
	}
	if err := json.Unmarshal(upd, &head); err != nil || head.SessionUpdate != "agent_message_chunk" || head.MessageID != "m1" {
		t.Fatalf("update head: %v %+v", err, head)
	}
}

func TestDecodeRequestPermissionParams(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s1","toolCall":{"toolCallId":"c1","title":"Write file","kind":"edit"},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"}]}`)
	p, err := decodeRequestPermissionParams(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.SessionID != "s1" || p.ToolCall.ToolCallID != "c1" || len(p.Options) != 1 || p.Options[0].OptionID != "allow" {
		t.Fatalf("unexpected params: %+v", p)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/acp/ -run 'TestDecode' -v`
Expected: 编译失败（类型/函数未定义）。

- [ ] **Step 3: 实现 protocol.go**

```go
package acp

import "encoding/json"

// Spec 描述如何启动 ACP agent 子进程（已完成平台 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// Implementation 是 initialize 的 clientInfo/agentInfo。
type Implementation struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type ClientCapabilities struct{} // v1 不声明任何可选能力

type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         Implementation     `json:"clientInfo"`
}

type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
}

type AuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type InitializeResult struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	AgentCapabilities  AgentCapabilities  `json:"agentCapabilities"`
	AgentInfo          Implementation     `json:"agentInfo"`
	AuthMethods        []AuthMethod       `json:"authMethods"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type NewSessionParams struct {
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
}

type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

type LoadSessionParams struct {
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
}

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResult struct {
	StopReason string `json:"stopReason"`
}

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// ToolCall 同时用于 request_permission 的 toolCall 与 session/update 的 tool_call。
type ToolCall struct {
	ToolCallID string             `json:"toolCallId"`
	Name       string             `json:"name,omitempty"`
	Title      string             `json:"title,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	Status     string             `json:"status,omitempty"`
	Content    []json.RawMessage  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage    `json:"rawOutput,omitempty"`
}

type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCall           `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type PermissionOutcome struct {
	Outcome  string `json:"outcome"` // "selected" | "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// updateParams 是 session/update 的参数信封。
type updateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

func decodeUpdateParams(raw json.RawMessage) (string, json.RawMessage, error) {
	var p updateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", nil, err
	}
	return p.SessionID, p.Update, nil
}

func decodeRequestPermissionParams(raw json.RawMessage) (RequestPermissionParams, error) {
	var p RequestPermissionParams
	err := json.Unmarshal(raw, &p)
	return p, err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/acp/ -run 'TestDecode' -v`
Expected: PASS。

---

### Task 3: Agent 子进程交互（agent.go）

**Files:**
- Create: `internal/acp/agent.go`
- Test: `internal/acp/agent_test.go`

- [ ] **Step 1: 写失败测试（用假 agent 进程脚本）**

测试用「test helper 进程」模式：`TestMain` 中当环境变量 `ACP_FAKE_AGENT` 命中时，直接运行假 agent（读 stdin 逐行、按方法返回脚本化结果），然后 `os.Exit`；否则跑正常测试。

```go
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
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
			continue // 假 agent 不发起请求
		}
		reply := func(result any) {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(*m.ID), "result": result})
		}
		switch m.Method {
		case "initialize":
			reply(map[string]any{
				"protocolVersion": 1,
				"agentCapabilities": map[string]any{"loadSession": true},
				"agentInfo": map[string]any{"name": "fake"},
			})
		case "session/new":
			reply(map[string]any{"sessionId": "sess-new"})
		case "session/load":
			// 先重放一条历史，再回响应
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-load",
				"update":    map[string]any{"sessionUpdate": "user_message_chunk", "messageId": "m0", "content": map[string]any{"type": "text", "text": "hello"}},
			}})
			reply(map[string]any{})
		case "session/prompt":
			// 发一条消息分片 + 一个权限请求，再回 stopReason
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-new",
				"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "m1", "content": map[string]any{"type": "text", "text": "working"}},
			}})
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "session/request_permission", "params": map[string]any{
				"sessionId": "sess-new",
				"toolCall":  map[string]any{"toolCallId": "c1", "title": "Write"},
				"options":   []any{map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"}},
			}})
			// 等客户端对权限请求的响应（id=99）到达后再回 prompt
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
	updates  []string // sessionID + sessionUpdate 摘要
	permDone chan PermissionOutcome
}

func (h *captureHandler) SessionUpdate(sessionID string, update json.RawMessage) {
	var head struct{ SessionUpdate string `json:"sessionUpdate"` }
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

// fakeAgentSpec 返回启动本测试二进制的 Spec（-test.run 到 TestMain 的进程）。
func fakeAgentSpec(t *testing.T) Spec {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Path: exe, Args: []string{"-test.run=TestMain"}, Env: []string{"ACP_FAKE_AGENT=1"}}
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
	if len(h.updates) < 2 { // prompt 期间至少一条 agent_message_chunk
		t.Fatalf("updates = %v", h.updates)
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
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.updates) == 0 || !strings.HasPrefix(h.updates[0], "sess-load:user_message_chunk") {
		t.Fatalf("replay updates = %v", h.updates)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/acp/ -run TestAgent -v`
Expected: 编译失败（`NewAgent`、`Handler` 未定义）。

- [ ] **Step 3: 实现 agent.go**

```go
package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"

	"github.com/yangk/kshell/internal/executil"
)

// Handler 接收 agent 主动发来的通知与反向请求。实现方在独立 goroutine 中被调用，
// 因此 RequestPermission 可以阻塞等待用户，而不阻塞读循环。
type Handler interface {
	SessionUpdate(sessionID string, update json.RawMessage)
	RequestPermission(ctx context.Context, sessionID, requestID string, p RequestPermissionParams) (PermissionOutcome, error)
}

// Agent 是一个已启动的 ACP agent 连接（一个子进程）。
type Agent struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	codec  *codec
	stderr *bytes.Buffer
	handler Handler

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan message
	closed  bool
	waitOnce sync.Once
	exitCode int
	waitErr  error
	done     chan struct{}
}

// NewAgent 启动子进程并返回连接；调用方负责 Close。
func NewAgent(ctx context.Context, spec Spec, h Handler) (*Agent, error) {
	if spec.Path == "" {
		return nil, errors.New("acp: 未指定 agent 可执行文件")
	}
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	executil.HideWindow(cmd)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(environ(), spec.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 ACP agent 失败: %w", err)
	}
	a := &Agent{
		cmd:     cmd,
		stdin:   stdin,
		codec:   newCodec(bufio.NewReaderSize(stdout, 1<<20), stdin),
		stderr:  &stderr,
		handler: h,
		pending: make(map[string]chan message),
		done:    make(chan struct{}),
	}
	go a.readLoop()
	go a.waitLoop()
	return a, nil
}

func (a *Agent) readLoop() {
	for {
		m, err := a.codec.read()
		if err != nil {
			a.failPending(err)
			return
		}
		a.dispatch(m)
	}
}

// dispatch 按“响应 / 通知 / 反向请求”分流。
func (a *Agent) dispatch(m message) {
	switch {
	case m.Method == "" && m.ID != nil: // 响应
		a.mu.Lock()
		ch := a.pending[*m.ID]
		delete(a.pending, *m.ID)
		a.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	case m.Method == "" && m.ID == nil: // 忽略
	default:
		go a.handleAgentMessage(m)
	}
}

// handleAgentMessage 处理 agent → client 的通知与反向请求（独立 goroutine，避免阻塞读循环）。
func (a *Agent) handleAgentMessage(m message) {
	switch m.Method {
	case "session/update":
		if a.handler == nil {
			return
		}
		sid, upd, err := decodeUpdateParams(m.Params)
		if err != nil {
			return
		}
		a.handler.SessionUpdate(sid, upd)
	default:
		if m.ID == nil {
			return // 未知通知：忽略
		}
		// 已知反向请求：session/request_permission
		if m.Method == "session/request_permission" && a.handler != nil {
			p, err := decodeRequestPermissionParams(m.Params)
			if err != nil {
				a.writeResponse(*m.ID, nil, &RPCError{Code: -32602, Message: "invalid params"})
				return
			}
			out, err := a.handler.RequestPermission(context.Background(), p.SessionID, *m.ID, p)
			if err != nil {
				a.writeResponse(*m.ID, nil, &RPCError{Code: -32603, Message: err.Error()})
				return
			}
			a.writeResponse(*m.ID, RequestPermissionResult{Outcome: out}, nil)
			return
		}
		a.writeResponse(*m.ID, nil, &RPCError{Code: -32601, Message: "method not found: " + m.Method})
	}
}

func (a *Agent) writeResponse(id string, result any, rpcErr *RPCError) {
	env := responseEnvelope{JSONRPC: jsonrpcVersion, ID: id, Result: result, Error: rpcErr}
	if result == nil && rpcErr == nil {
		env.Result = struct{}{}
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	_ = a.codec.write(env)
}

// call 发一个请求并等响应；ctx 取消时返回错误（不等待迟到响应）。
func (a *Agent) call(ctx context.Context, method string, params, out any) error {
	a.mu.Lock()
	a.nextID++
	id := strconv.FormatInt(a.nextID, 10)
	ch := make(chan message, 1)
	a.pending[id] = ch
	a.mu.Unlock()

	a.writeMu.Lock()
	err := a.codec.write(requestEnvelope{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: params})
	a.writeMu.Unlock()
	if err != nil {
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return ctx.Err()
	case <-a.done:
		return errors.New("acp: 连接已关闭")
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	}
}

func (a *Agent) notify(method string, params any) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.codec.write(notificationEnvelope{JSONRPC: jsonrpcVersion, Method: method, Params: params})
}

func (a *Agent) Initialize(ctx context.Context) (InitializeResult, error) {
	var res InitializeResult
	params := InitializeParams{
		ProtocolVersion: 1,
		ClientInfo:      Implementation{Name: "kshell", Title: "kshell", Version: "0.1.0"},
	}
	err := a.call(ctx, "initialize", params, &res)
	return res, err
}

func (a *Agent) NewSession(ctx context.Context, cwd string) (string, error) {
	res, err := a.newOrLoad(ctx, "session/new", NewSessionParams{Cwd: cwd, McpServers: []any{}})
	return res.SessionID, err
}

func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string) error {
	_, err := a.newOrLoad(ctx, "session/load", LoadSessionParams{SessionID: sessionID, Cwd: cwd, McpServers: []any{}})
	return err
}

func (a *Agent) newOrLoad(ctx context.Context, method string, params any) (NewSessionResult, error) {
	var res NewSessionResult
	err := a.call(ctx, method, params, &res)
	return res, err
}

func (a *Agent) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	var res PromptResult
	params := PromptParams{SessionID: sessionID, Prompt: []ContentBlock{{Type: "text", Text: text}}}
	if err := a.call(ctx, "session/prompt", params, &res); err != nil {
		return "", err
	}
	return res.StopReason, nil
}

func (a *Agent) Cancel(sessionID string) error {
	return a.notify("session/cancel", CancelParams{SessionID: sessionID})
}

func (a *Agent) Stderr() string { return a.stderr.String() }

func (a *Agent) waitLoop() {
	err := a.cmd.Wait()
	a.mu.Lock()
	a.waitErr = err
	if a.cmd.ProcessState != nil {
		a.exitCode = a.cmd.ProcessState.ExitCode()
	}
	a.mu.Unlock()
	a.failPending(err)
}

func (a *Agent) failPending(err error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	close(a.done)
	for id, ch := range a.pending {
		delete(a.pending, id)
		close(ch)
	}
	a.mu.Unlock()
}

// Wait 返回退出码与等待错误（重复调用返回同一次结果）。仅真实进程实现需要。
func (a *Agent) Wait() (int, error) {
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitCode, a.waitErr
}

func (a *Agent) Close() error {
	a.waitOnce.Do(func() {
		_ = a.stdin.Close() // 关 stdin 让 agent 自然退出
		if a.cmd.Process != nil {
			_ = a.cmd.Process.Kill() // 兜底：不退出就杀
		}
	})
	a.failPending(io.EOF)
	return nil
}

// environ 抽象 os.Environ，便于测试替换；默认实现。
var environ = func() []string { return nil }
```

在 `agent.go` 底部或单独 `environ_default.go` 把 `environ` 默认设为 `os.Environ`：

```go
// agent_default.go (package acp)
package acp

import "os"

func init() { environ = os.Environ }
```

注意：`call` 在 `<-a.done` 与 `<-ch` 上都可能取；`failPending` 会 close(ch) 导致 `<-ch` 收到零值 message（无 Error、无 Result），此时 `out` 保持不变、返回 nil —— 需修正：`call` 在 `<-a.done` 与 `<-ch` 同时就绪时可能选到 ch 的零值。**实现时把 `close(ch)` 改为向 ch 发一个显式错误 message**（`ch <- message{Error: &RPCError{Code:-32000, Message:"connection closed"}}`），避免零值误判成功。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/acp/ -run TestAgent -v`
Expected: PASS。

- [ ] **Step 5: 跑整包**

Run: `go vet ./internal/acp/ ; go test ./internal/acp/ -count=1`
Expected: 全绿。

---

## M2：编排层 internal/chat

### Task 4: 模型与 Backend 接口（types.go / backend.go）

**Files:**
- Create: `internal/chat/types.go`
- Create: `internal/chat/backend.go`
- Test: `internal/chat/manager_test.go`（先放 fake conn 与首个用例）

- [ ] **Step 1: 写 types.go**

```go
// Package chat 编排 ACP agent 进程与会话：起进程、握手、new/load 会话、
// 把 session/update 归一化成前端时间线、转发权限请求。Backend 可打桩以便测试。
package chat

import (
	"context"
	"encoding/json"

	"github.com/yangk/kshell/internal/acp"
)

const (
	KindSession = "session"
	KindNew     = "new"

	StatusStarting = "starting"
	StatusReady    = "ready"
	StatusRunning  = "running"
	StatusExited   = "exited"
)

type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type Info struct {
	ID        string
	Kind      string
	SessionID string
	Workspace string
	Title     string
	ToolID    string
	Status    string
	ExitCode  int
	Error     string
}

// ToolCall 是发给前端的工具调用快照。
type ToolCall struct {
	ToolCallID string          `json:"ToolCallID"`
	Name       string          `json:"Name,omitempty"`
	Title      string          `json:"Title,omitempty"`
	Kind       string          `json:"Kind,omitempty"`
	Status     string          `json:"Status,omitempty"`
	Content    []json.RawMessage `json:"Content,omitempty"`
	RawInput   json.RawMessage `json:"RawInput,omitempty"`
	RawOutput  json.RawMessage `json:"RawOutput,omitempty"`
}

type PlanEntry struct {
	Content  string `json:"Content"`
	Priority string `json:"Priority,omitempty"`
	Status   string `json:"Status,omitempty"`
}

// Update 是归一化时间线事件，带单调 Seq 供前端去重。
type Update struct {
	Seq        int64      `json:"Seq"`
	Type       string     `json:"Type"` // user | assistant | thought | tool | plan | turn_done | error
	MessageID  string     `json:"MessageID,omitempty"`
	Text       string     `json:"Text,omitempty"`
	ToolCallID string     `json:"ToolCallID,omitempty"`
	Tool       *ToolCall  `json:"Tool,omitempty"`
	Plan       []PlanEntry `json:"Plan,omitempty"`
	StopReason string     `json:"StopReason,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"OptionID"`
	Name     string `json:"Name"`
	Kind     string `json:"Kind"`
}

type PermissionRequest struct {
	RequestID string             `json:"RequestID"`
	SessionID string             `json:"SessionID"`
	ToolCall  ToolCall           `json:"ToolCall"`
	Options   []PermissionOption `json:"Options"`
}

// Conn 是 ACP 连接的高层封装；真实实现是 acp.Agent，测试注入假实现。
type Conn interface {
	Initialize(ctx context.Context) (acp.InitializeResult, error)
	NewSession(ctx context.Context, cwd string) (string, error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	Prompt(ctx context.Context, sessionID, text string) (string, error)
	Cancel(sessionID string) error
	Wait() (int, error)
	Close() error
}

// Backend 启动一条 ACP 连接；可打桩。
type Backend interface {
	Start(spec Spec, h acp.Handler) (Conn, error)
}
```

- [ ] **Step 2: 写 backend.go**

```go
package chat

import (
	"context"

	"github.com/yangk/kshell/internal/acp"
)

// RealBackend 用 internal/acp 启动真实 agent 进程。
type RealBackend struct{}

func (RealBackend) Start(spec Spec, h acp.Handler) (Conn, error) {
	return acp.NewAgent(context.Background(), acp.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, h)
}
```

- [ ] **Step 3: 编译**

Run: `go build ./internal/chat/`
Expected: 成功。

（本任务无独立测试；Manager 测试在 Task 5 覆盖 Backend 打桩。）

---

### Task 5: Manager（manager.go）

**Files:**
- Create: `internal/chat/manager.go`
- Test: `internal/chat/manager_test.go`

- [ ] **Step 1: 写 manager_test.go（假 Conn + 全生命周期用例）**

```go
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

// fakeConn 按脚本记录调用，并可在 Prompt 时触发回调。
type fakeConn struct {
	mu        sync.Mutex
	newID     string
	loadErr   error
	promptErr error
	stop      string
	cancelled []string
	closed    bool
	handler   acp.Handler
	waitCh    chan struct{}
	waitCode  int
}

func (c *fakeConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	return acp.InitializeResult{ProtocolVersion: 1, AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (c *fakeConn) NewSession(ctx context.Context, cwd string) (string, error) {
	if c.newID == "" {
		c.newID = "sess-new"
	}
	return c.newID, nil
}
func (c *fakeConn) LoadSession(ctx context.Context, sessionID, cwd string) error { return c.loadErr }
func (c *fakeConn) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	if c.stop == "" {
		c.stop = "end_turn"
	}
	return c.stop, c.promptErr
}
func (c *fakeConn) Cancel(sessionID string) error {
	c.mu.Lock()
	c.cancelled = append(c.cancelled, sessionID)
	c.mu.Unlock()
	return nil
}
func (c *fakeConn) Wait() (int, error) {
	if c.waitCh != nil {
		<-c.waitCh
	}
	return c.waitCode, nil
}
func (c *fakeConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.waitCh != nil {
		select {
		case <-c.waitCh:
		default:
			close(c.waitCh)
		}
	}
	return nil
}

type fakeBackend struct {
	conn *fakeConn
	err  error
	last acp.Handler
}

func (b *fakeBackend) Start(spec Spec, h acp.Handler) (Conn, error) {
	b.last = h
	if b.err != nil {
		return nil, b.err
	}
	b.conn.handler = h
	return b.conn, nil
}

func newTestManager(t *testing.T) (*Manager, *fakeBackend, *collector) {
	t.Helper()
	b := &fakeBackend{conn: &fakeConn{}}
	c := &collector{}
	m := NewManager(b, c.onUpdate, c.onPermission, c.onExit)
	return m, b, c
}

type collector struct {
	mu         sync.Mutex
	updates    []Update
	permission *PermissionRequest
	exitCode   int
	exited     bool
	permCh     chan string // 前端回应 optionId
}

func (c *collector) onUpdate(id string, u Update) {
	c.mu.Lock()
	c.updates = append(c.updates, u)
	c.mu.Unlock()
}
func (c *collector) onPermission(id string, r PermissionRequest) {
	c.mu.Lock()
	c.permission = &r
	c.mu.Unlock()
	if c.permCh != nil {
		c.permCh <- r.RequestID
	}
}
func (c *collector) onExit(id string, code int, errMsg string) {
	c.mu.Lock()
	c.exited = true
	c.exitCode = code
	c.mu.Unlock()
}

func TestManagerOpenNewAndPrompt(t *testing.T) {
	m, _, c := newTestManager(t)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w", Title: "新会话"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if info.Status != StatusReady || info.SessionID != "sess-new" {
		t.Fatalf("info = %+v", info)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	// 等 turn_done
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, u := range c.updates {
			if u.Type == "turn_done" {
				c.mu.Unlock()
				return
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no turn_done; updates=%+v", c.updates)
}

func TestManagerLoadReplay(t *testing.T) {
	m, b, c := newTestManager(t)
	info, err := m.Open("session:s1", Info{Kind: KindSession, SessionID: "s1", Workspace: "/w"}, Spec{Path: "x"}, "s1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 模拟 load 期间 agent 重放一条历史
	raw := json.RawMessage(`{"sessionUpdate":"user_message_chunk","messageId":"m0","content":{"type":"text","text":"old"}}`)
	b.last.SessionUpdate("s1", raw)
	if got := m.History(info.ID); len(got) == 0 || got[0].Type != "user" {
		t.Fatalf("history = %+v", got)
	}
	_ = c
}

func TestManagerPermissionRoundTrip(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 在独立 goroutine 模拟 agent 请求权限
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "77", acp.RequestPermissionParams{
			SessionID: "sess-new",
			ToolCall:  acp.ToolCall{ToolCallID: "c1", Title: "Write"},
			Options:   []acp.PermissionOption{{OptionID: "allow", Name: "Allow", Kind: "allow_once"}},
		})
		done <- out
	}()
	reqID := <-c.permCh
	if err := m.RespondPermission(info.ID, reqID, "allow"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	select {
	case out := <-done:
		if out.Outcome != "selected" || out.OptionID != "allow" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission not resolved")
	}
}

func TestManagerCloseCancelsPendingPermission(t *testing.T) {
	m, b, c := newTestManager(t)
	c.permCh = make(chan string, 1)
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	done := make(chan acp.PermissionOutcome, 1)
	go func() {
		out, _ := b.last.RequestPermission(context.Background(), "sess-new", "88", acp.RequestPermissionParams{SessionID: "sess-new"})
		done <- out
	}()
	reqID := <-c.permCh
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case out := <-done:
		if out.Outcome != "cancelled" {
			t.Fatalf("outcome %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending permission not cancelled on close")
	}
	_ = reqID
}

func TestManagerOpenBackendError(t *testing.T) {
	b := &fakeBackend{conn: &fakeConn{}, err: errors.New("boom")}
	m := NewManager(b, nil, nil, nil)
	if _, err := m.Open("new:1", Info{}, Spec{Path: "x"}, ""); err == nil {
		t.Fatal("want error")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/chat/ -run TestManager -v`
Expected: 编译失败（Manager/NewManager 未定义）。

- [ ] **Step 3: 实现 manager.go**

```go
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	errNoBackend = errors.New("chat: 后端未装配")
	errNotFound  = errors.New("chat: 会话不存在或已关闭")
	errRunning   = errors.New("chat: 上一轮尚未结束")
)

type session struct {
	id        string
	info      Info
	conn      Conn
	sessionID string
	handler   *sessionHandler

	seq     int64
	history []Update

	mu      sync.Mutex
	pending map[string]chan permissionResult // requestID → 结果
	exited  bool
	cancel  context.CancelFunc
}

type permissionResult struct {
	outcome string
	option  string
}

type sessionHandler struct {
	m  *Manager
	id string
}

type Manager struct {
	backend      Backend
	onUpdate     func(id string, u Update)
	onPermission func(id string, r PermissionRequest)
	onExit       func(id string, code int, errMsg string)

	mu      sync.Mutex
	byKey   map[string]*session
	byID    map[string]*session
	order   []*session
	nextSeq int
}

func NewManager(b Backend,
	onUpdate func(id string, u Update),
	onPermission func(id string, r PermissionRequest),
	onExit func(id string, code int, errMsg string)) *Manager {
	return &Manager{
		backend: b, onUpdate: onUpdate, onPermission: onPermission, onExit: onExit,
		byKey: make(map[string]*session), byID: make(map[string]*session),
	}
}

// Open 起进程并完成 initialize + session/new|load；sessionID 非空则 load。
func (m *Manager) Open(key string, info Info, spec Spec, sessionID string) (Info, error) {
	if m.backend == nil {
		return Info{}, errNoBackend
	}
	if key == "" {
		return Info{}, errors.New("chat: key 不能为空")
	}

	m.mu.Lock()
	if s, ok := m.byKey[key]; ok {
		if !s.exited {
			out := s.info
			m.mu.Unlock()
			return out, nil
		}
		// 旧会话已退出：清理两张表后再重建，避免 byID 泄漏
		delete(m.byKey, key)
		delete(m.byID, s.id)
		if s.conn != nil {
			go s.conn.Close()
		}
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.nextSeq++
	s := &session{
		id:      fmt.Sprintf("c%d", m.nextSeq),
		info:    info,
		handler: &sessionHandler{m: m},
		pending: make(map[string]chan permissionResult),
		cancel:  cancel,
	}
	s.info.ID = s.id
	s.info.Status = StatusStarting
	s.handler.id = s.id
	m.byKey[key] = s
	m.byID[s.id] = s
	m.order = append(m.order, s)
	m.mu.Unlock()

	conn, err := m.backend.Start(spec, s.handler)
	if err != nil {
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}
	s.conn = conn

	if _, err := conn.Initialize(ctx); err != nil {
		_ = conn.Close()
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}

	if sessionID != "" {
		if err := conn.LoadSession(ctx, sessionID, info.Workspace); err != nil {
			_ = conn.Close()
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
		s.sessionID = sessionID
	} else {
		sid, err := conn.NewSession(ctx, info.Workspace)
		if err != nil {
			_ = conn.Close()
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
		s.sessionID = sid
	}

	m.mu.Lock()
	s.info.SessionID = s.sessionID
	s.info.Status = StatusReady
	out := s.info
	m.mu.Unlock()

	go m.watchExit(s)
	return out, nil
}

// watchExit 等进程退出并在未主动关闭时回调 onExit。
func (m *Manager) watchExit(s *session) {
	code, _ := s.conn.Wait()
	m.mu.Lock()
	if s.exited {
		m.mu.Unlock()
		return
	}
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	out := s.info
	m.mu.Unlock()
	if m.onExit != nil {
		m.onExit(s.id, code, "")
	}
	_ = out
}

func (m *Manager) fail(id string, code int, msg string) {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	s.info.Error = msg
	m.mu.Unlock()
	if m.onExit != nil {
		m.onExit(id, code, msg)
	}
}

func (m *Manager) Prompt(id, text string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	if s.info.Status == StatusRunning {
		m.mu.Unlock()
		return errRunning
	}
	if s.exited {
		m.mu.Unlock()
		return errNotFound
	}
	s.info.Status = StatusRunning
	sid := s.sessionID
	conn := s.conn
	m.mu.Unlock()

	go func() {
		stop, err := conn.Prompt(context.Background(), sid, text)
		m.mu.Lock()
		if cur := m.byID[id]; cur == s {
			s.info.Status = StatusReady
			s.seq++
			u := Update{Seq: s.seq, Type: "turn_done", StopReason: stop}
			if err != nil {
				u.Type = "error"
				u.Text = err.Error()
			}
			s.history = append(s.history, u)
			m.mu.Unlock()
			if m.onUpdate != nil {
				m.onUpdate(id, u)
			}
			return
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	sid := s.sessionID
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	m.mu.Unlock()

	for reqID, ch := range pending {
		_ = reqID
		select {
		case ch <- permissionResult{outcome: "cancelled"}:
		default:
		}
	}
	if sid != "" {
		_ = s.conn.Cancel(sid)
	}
	return nil
}

func (m *Manager) RespondPermission(id, requestID, optionID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "selected", option: optionID})
}

func (m *Manager) CancelPermission(id, requestID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "cancelled"})
}

func (m *Manager) resolvePermission(id, requestID string, r permissionResult) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	ch := s.pending[requestID]
	delete(s.pending, requestID)
	m.mu.Unlock()
	if ch == nil {
		return errNotFound
	}
	ch <- r
	return nil
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	s.exited = true
	s.info.Status = StatusExited
	if s.cancel != nil {
		s.cancel()
	}
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	conn := s.conn
	m.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- permissionResult{outcome: "cancelled"}:
		default:
		}
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*session, len(m.order))
	copy(all, m.order)
	m.mu.Unlock()
	for _, s := range all {
		_ = m.Close(s.id)
	}
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, s := range m.order {
		out = append(out, s.info)
	}
	return out
}

func (m *Manager) History(id string) []Update {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return nil
	}
	out := make([]Update, len(s.history))
	copy(out, s.history)
	return out
}

// --- sessionHandler（acp.Handler）---

func (h *sessionHandler) SessionUpdate(sessionID string, raw json.RawMessage) {
	h.m.onSessionUpdate(h.id, sessionID, raw)
}

func (h *sessionHandler) RequestPermission(ctx context.Context, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	return h.m.awaitPermission(h.id, sessionID, requestID, p)
}
```

（`onSessionUpdate`、`awaitPermission`、`normalizeUpdate`、`toToolCall` 在 Task 5 Step 4 追加到同一文件。）

- [ ] **Step 4: 实现 update 归一化与权限等待（追加到 manager.go）**

```go
// acpUpdate 按 ACP 规范的小写字段解码 session/update（前端模型是 PascalCase，故单独解码再转换）。
type acpUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	MessageID     string          `json:"messageId"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
	Entries       []acpPlanEntry  `json:"entries"`
}

type acpPlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// normalizeUpdate 把 ACP session/update 转成前端时间线事件；不支持的变体返回 ok=false。
func normalizeUpdate(raw json.RawMessage) (Update, bool) {
	var u acpUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return Update{}, false
	}
	switch u.SessionUpdate {
	case "user_message_chunk":
		return Update{Type: "user", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_message_chunk":
		return Update{Type: "assistant", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_thought_chunk":
		return Update{Type: "thought", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "tool_call", "tool_call_update":
		if u.ToolCallID == "" {
			return Update{}, false
		}
		// tool content 是数组，单独再解一次（不能用上面的 singleton Content）
		var content []json.RawMessage
		var probe struct{ Content []json.RawMessage `json:"content"` }
		if err := json.Unmarshal(raw, &probe); err == nil {
			content = probe.Content
		}
		tc := ToolCall{
			ToolCallID: u.ToolCallID, Name: u.Name, Title: u.Title,
			Kind: u.Kind, Status: u.Status, Content: content,
			RawInput: u.RawInput, RawOutput: u.RawOutput,
		}
		return Update{Type: "tool", ToolCallID: tc.ToolCallID, Tool: &tc}, true
	case "plan":
		plan := make([]PlanEntry, 0, len(u.Entries))
		for _, e := range u.Entries {
			plan = append(plan, PlanEntry{Content: e.Content, Priority: e.Priority, Status: e.Status})
		}
		return Update{Type: "plan", Plan: plan}, true
	default:
		return Update{}, false // 未知变体忽略（前向兼容）
	}
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &block); err == nil && block.Text != "" {
		return block.Text
	}
	// 容错：content 可能是纯字符串
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func (m *Manager) onSessionUpdate(id, sessionID string, raw json.RawMessage) {
	u, ok := normalizeUpdate(raw)
	if !ok {
		return
	}
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	s.seq++
	u.Seq = s.seq
	s.history = append(s.history, u)
	m.mu.Unlock()
	if m.onUpdate != nil {
		m.onUpdate(id, u)
	}
}

func (m *Manager) awaitPermission(id, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	ch := make(chan permissionResult, 1)
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
	s.pending[requestID] = ch
	m.mu.Unlock()

	req := PermissionRequest{RequestID: requestID, SessionID: sessionID, ToolCall: toToolCall(p.ToolCall)}
	for _, o := range p.Options {
		req.Options = append(req.Options, PermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	if m.onPermission != nil {
		m.onPermission(id, req)
	}

	r := <-ch
	if r.outcome == "cancelled" {
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
	return acp.PermissionOutcome{Outcome: "selected", OptionID: r.option}, nil
}

func toToolCall(t acp.ToolCall) ToolCall {
	return ToolCall{
		ToolCallID: t.ToolCallID,
		Name:       t.Name,
		Title:      t.Title,
		Kind:       t.Kind,
		Status:     t.Status,
		Content:    t.Content,
		RawInput:   t.RawInput,
		RawOutput:  t.RawOutput,
	}
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go vet ./internal/chat/ ; go test ./internal/chat/ -count=1 -v`
Expected: 全绿（5 个用例）。

关联性修正：`fakeConn.Cancel` 记 `cancelled` 时未加锁写 `cancelled` 字段本身已加锁；`TestManagerCloseCancelsPendingPermission` 断言 outcome。

---

## M3：Provider / discovery / launch

### Task 6: providers ACP 探测

**Files:**
- Create: `internal/providers/acp.go`
- Test: `internal/providers/acp_test.go`
- Modify: `internal/providers/claude.go`

- [ ] **Step 1: 写失败测试**

```go
package providers

import "testing"

func TestDetectACP_PathHit(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if name == "claude-agent-acp" {
			return "/usr/bin/claude-agent-acp", nil
		}
		return "", errNotFoundStub
	}
	defer func() { lookPathFn = prev }()

	d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}})
	if !d.Available || d.Source != "path" || d.BinPath != "/usr/bin/claude-agent-acp" {
		t.Fatalf("detection = %+v", d)
	}
}

func TestDetectACP_NpxFallback(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if name == "npx" {
			return "/usr/bin/npx", nil
		}
		return "", errNotFoundStub
	}
	defer func() { lookPathFn = prev }()

	d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}, NPMPackage: "@agentclientprotocol/claude-agent-acp"})
	if !d.Available || d.Source != "npx" || d.Package != "@agentclientprotocol/claude-agent-acp" {
		t.Fatalf("detection = %+v", d)
	}
}

func TestDetectACP_None(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(string) (string, error) { return "", errNotFoundStub }
	defer func() { lookPathFn = prev }()

	if d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}}); d.Available {
		t.Fatalf("want unavailable, got %+v", d)
	}
}

func TestClaudeACPAdapter(t *testing.T) {
	a := Claude{}.ACPAdapter()
	if len(a.BinNames) == 0 || a.NPMPackage == "" {
		t.Fatalf("adapter = %+v", a)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers/ -run 'TestDetectACP|TestClaudeACP' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现 acp.go**

```go
package providers

import "os/exec"

// errNotFoundStub 供测试注入使用。
var errNotFoundStub = exec.ErrNotFound

// lookPathFn 抽象 exec.LookPath，测试可替换。
var lookPathFn = exec.LookPath

// ACPAdapter 声明某工具的 ACP 适配器来源。
type ACPAdapter struct {
	BinNames   []string
	NPMPackage string
	ExtraArgs  []string
}

// ACPProvider 可选接口：实现即表示该工具可用 ACP 交互。
type ACPProvider interface {
	ACPAdapter() ACPAdapter
}

// ACPDetection 是 ACP 适配器可用性探测结果。
type ACPDetection struct {
	Available bool   `json:"Available"`
	Source    string `json:"Source"`
	BinPath   string `json:"BinPath,omitempty"`
	Package   string `json:"Package,omitempty"`
}

// DetectACP 依次探测 BinNames，全未命中时用 npx 兜底。
func DetectACP(a ACPAdapter) ACPDetection {
	for _, name := range a.BinNames {
		if bin, err := lookPathFn(name); err == nil && bin != "" {
			return ACPDetection{Available: true, Source: "path", BinPath: bin}
		}
	}
	if a.NPMPackage != "" {
		for _, npx := range npxNames() {
			if bin, err := lookPathFn(npx); err == nil && bin != "" {
				return ACPDetection{Available: true, Source: "npx", BinPath: bin, Package: a.NPMPackage}
			}
		}
	}
	return ACPDetection{}
}
```

`npxNames()` 放平台文件：`acp_windows.go` 返回 `[]string{"npx.cmd", "npx"}`；`acp_other.go`（build tag `!windows`）返回 `[]string{"npx"}`。

```go
// acp_windows.go
//go:build windows
package providers
func npxNames() []string { return []string{"npx.cmd", "npx"} }

// acp_other.go
//go:build !windows
package providers
func npxNames() []string { return []string{"npx"} }
```

- [ ] **Step 4: 在 claude.go 增加实现**

```go
// ACPAdapter 让 Claude 走 Zed 的 ACP 适配器；新名优先，旧名兼容探测。
func (Claude) ACPAdapter() ACPAdapter {
	return ACPAdapter{
		BinNames:   []string{"claude-agent-acp", "claude-code-acp"},
		NPMPackage: "@agentclientprotocol/claude-agent-acp",
	}
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/providers/ -count=1`
Expected: 全绿。

---

### Task 7: discovery 暴露 ACP 可用性

**Files:**
- Modify: `internal/discovery/tools.go`
- Test: `internal/discovery/tools_test.go`（若无则新建）

- [ ] **Step 1: 写失败测试**

```go
package discovery

import (
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

type acpTestProvider struct{ providers.Claude }

func TestDetectAll_FillsACP(t *testing.T) {
	tools := DetectAll("", []providers.Provider{providers.Claude{}})
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].ACP == nil {
		t.Fatal("ACP detection not attached")
	}
}
```

（`providers.Claude` 实现 `Provider` 与 `ACPProvider`；断言只要求字段存在，不依赖本机是否装适配器。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/discovery/ -run TestDetectAll_FillsACP -v`
Expected: 编译失败（`Tool.ACP` 未定义）。

- [ ] **Step 3: 修改 tools.go**

在 `Tool` 结构增加字段：

```go
type Tool struct {
	ID        string
	Name      string
	BinPath   string
	Version   string
	Installed bool
	Source    string
	ACP       *providers.ACPDetection
}
```

在 `detectAll` 的循环里，构造 `t` 后：

```go
if ap, ok := p.(providers.ACPProvider); ok {
	d := providers.DetectACP(ap.ACPAdapter())
	t.ACP = &d
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/discovery/ -count=1`
Expected: 全绿。

---

### Task 8: launch ACP 启动描述

**Files:**
- Modify: `internal/launch/launch.go`
- Test: `internal/launch/launch_test.go`

- [ ] **Step 1: 写失败测试**

```go
package launch

import (
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func acpTools(t *testing.T, det providers.ACPDetection) []discovery.Tool {
	return []discovery.Tool{{ID: "claude", Name: "Claude Code", Installed: true, BinPath: "claude", ACP: &det}}
}

func TestForWorkspaceACP_NpxFallback(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(t, providers.ACPDetection{Available: true, Source: "npx", Package: "@agentclientprotocol/claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude")
	if err != nil {
		t.Fatalf("for workspace acp: %v", err)
	}
	if l.Path != "npx" || len(l.Args) != 2 || l.Args[0] != "-y" {
		t.Fatalf("launch = %+v", l)
	}
	if l.Dir != "/w" {
		t.Fatalf("dir = %q", l.Dir)
	}
}

func TestForWorkspaceACP_PathHit(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(t, providers.ACPDetection{Available: true, Source: "path", BinPath: "/usr/bin/claude-agent-acp"})
	l, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "/usr/bin/claude-agent-acp" {
		t.Fatalf("launch = %+v", l)
	}
}

func TestForWorkspaceACP_Unavailable(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(t, providers.ACPDetection{Available: false})
	if _, err := ForWorkspaceACP(ps, tools, discovery.Workspace{Path: "/w"}, "claude"); err == nil {
		t.Fatal("want error when ACP unavailable")
	}
}

func TestForSessionACP_UsesWorkspaceDir(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := acpTools(t, providers.ACPDetection{Available: true, Source: "path", BinPath: "claude-agent-acp"})
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: "/proj"}
	l, err := ForSessionACP(ps, tools, s)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != "/proj" {
		t.Fatalf("dir = %q", l.Dir)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/launch/ -run 'TestForWorkspaceACP|TestForSessionACP' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现（追加到 launch.go）**

```go
// ErrACPUnavailable 表示目标工具没有可用的 ACP 适配器。
var ErrACPUnavailable = errors.New("该工具没有可用的 ACP 适配器")

// ForSessionACP 产出用 ACP 恢复历史会话的启动描述（命令来自适配器探测）。
func ForSessionACP(ps []providers.Provider, tools []discovery.Tool, s providers.Session) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return acpLaunch(tool, s.Workspace)
}

// ForWorkspaceACP 产出在工作区新建 ACP 会话的启动描述；toolID 为空时用首选工具。
func ForWorkspaceACP(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string) (providers.Launch, error) {
	var tool discovery.Tool
	if toolID == "" {
		_, t, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool = t
	} else {
		t, ok := toolFor(tools, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool = t
	}
	return acpLaunch(tool, ws.Path)
}

func acpLaunch(tool discovery.Tool, dir string) (providers.Launch, error) {
	if tool.ACP == nil || !tool.ACP.Available {
		return providers.Launch{}, ErrACPUnavailable
	}
	if tool.ACP.Source == "npx" {
		return providers.Launch{Path: tool.ACP.BinPath, Args: []string{"-y", tool.ACP.Package}, Dir: dir}, nil
	}
	return providers.Launch{Path: tool.ACP.BinPath, Dir: dir}, nil
}
```

注意：npx 分支用 `tool.ACP.BinPath`（探测到的 npx 可执行路径）而非字面量 "npx"，兼容 Windows `npx.cmd`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go vet ./internal/launch/ ; go test ./internal/launch/ -count=1`
Expected: 全绿。

---

## M4：桌面绑定与统一入口

### Task 9: desktop chat 绑定与事件

**Files:**
- Create: `internal/desktop/chat.go`
- Modify: `internal/desktop/app.go`
- Test: `internal/desktop/chat_test.go`

- [ ] **Step 1: 写失败测试**

```go
package desktop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
)

type fakeChatConn struct{}

func (fakeChatConn) Initialize(ctx context.Context) (acp.InitializeResult, error) {
	return acp.InitializeResult{AgentCapabilities: acp.AgentCapabilities{LoadSession: true}}, nil
}
func (fakeChatConn) NewSession(ctx context.Context, cwd string) (string, error) { return "s1", nil }
func (fakeChatConn) LoadSession(ctx context.Context, sessionID, cwd string) error { return nil }
func (fakeChatConn) Prompt(ctx context.Context, sessionID, text string) (string, error) { return "end_turn", nil }
func (fakeChatConn) Cancel(sessionID string) error { return nil }
func (fakeChatConn) Wait() (int, error)            { return 0, nil }
func (fakeChatConn) Close() error                  { return nil }

type fakeChatBackend struct{}

func (fakeChatBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return fakeChatConn{}, nil
}

func TestChatManagerEmitsUpdate(t *testing.T) {
	var mu sync.Mutex
	events := map[string]int{}
	emit := func(name string, data ...any) {
		mu.Lock()
		events[name]++
		mu.Unlock()
	}
	m := newChatManagerWith(emit, fakeChatBackend{})
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = info
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	// 等 turn_done → chat:update
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := events["chat:update"]
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no chat:update emitted")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/desktop/ -run TestChatManager -v`
Expected: 编译失败（`newChatManagerWith` 未定义）。

- [ ] **Step 3: 实现 chat.go**

```go
package desktop

import (
	"context"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// newChatManager 用真实后端装配聊天管理器（initRealDeps 用）。
func newChatManager(a *App) *chat.Manager {
	return newChatManagerWith(a.Emit, chat.RealBackend{})
}

// newChatManagerWith 装配聊天管理器：时间线/权限/退出经 emit 转发给前端。
func newChatManagerWith(emit func(name string, data ...any), b chat.Backend) *chat.Manager {
	return chat.NewManager(b,
		func(id string, u chat.Update) {
			emit("chat:update", map[string]any{"id": id, "update": u})
		},
		func(id string, r chat.PermissionRequest) {
			emit("chat:permission", map[string]any{"id": id, "request": r})
		},
		func(id string, code int, errMsg string) {
			emit("chat:exit", map[string]any{"id": id, "exitCode": code, "error": errMsg})
		})
}

func (a *App) chats() *chat.Manager { return a.snapshot().Chats }

// OpenSession 恢复历史会话：ACP 可用走聊天，否则回退终端。
func (a *App) OpenSession(sessionID string) (OpenedSession, error) {
	s, tools, ok := a.sessionByIDReady(sessionID)
	if !ok {
		return OpenedSession{}, errSessionNotFound
	}
	o := a.snapshot()
	m := a.chats()
	if m != nil {
		if l, err := launch.ForSessionACP(o.Providers, tools, s); err == nil {
			if info, err := openChat(m, "session:"+sessionID, chat.Info{
				Kind:      chat.KindSession,
				SessionID: s.ID,
				Workspace: s.Workspace,
				Title:     s.Title,
				ToolID:    s.ToolID,
			}, l, s.ID); err == nil {
				return OpenedSession{Kind: "chat", Chat: &info}, nil
			} else {
				return a.fallbackTerminalSession(sessionID, err.Error())
			}
		}
	}
	return a.fallbackTerminalSession(sessionID, "")
}

// OpenWorkspace 在工作区新建会话：ACP 可用走聊天，否则回退终端。
func (a *App) OpenWorkspace(wsID, toolID string) (OpenedSession, error) {
	ws, tools, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return OpenedSession{}, errWorkspaceNotFound
	}
	o := a.snapshot()
	m := a.chats()
	if m != nil {
		if l, err := launch.ForWorkspaceACP(o.Providers, tools, ws, toolID); err == nil {
			title := workspaceTerminalTitle(o.Providers, tools, ws, toolID)
			key := "new:" + nextChatSeq()
			if info, err := openChat(m, key, chat.Info{
				Kind:      chat.KindNew,
				Workspace: ws.Path,
				Title:     title,
				ToolID:    toolID,
			}, l, ""); err == nil {
				return OpenedSession{Kind: "chat", Chat: &info}, nil
			} else {
				return a.fallbackTerminalWorkspace(wsID, toolID, err.Error())
			}
		}
	}
	return a.fallbackTerminalWorkspace(wsID, toolID, "")
}

func openChat(m *chat.Manager, key string, info chat.Info, l providers.Launch, sessionID string) (chat.Info, error) {
	spec, err := launcher.Build(l)
	if err != nil {
		return chat.Info{}, err
	}
	return m.Open(key, info, chat.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, sessionID)
}

// OpenedSession 是统一打开结果：Kind 为 "chat" 或 "terminal"。
type OpenedSession struct {
	Kind     string         `json:"Kind"`
	Chat     *chat.Info     `json:"Chat,omitempty"`
	Terminal *terminal.Info `json:"Terminal,omitempty"`
	Fallback string         `json:"Fallback,omitempty"`
}

func (a *App) fallbackTerminalSession(sessionID, reason string) (OpenedSession, error) {
	info, err := a.OpenSessionTerminal(sessionID, 0, 0)
	if err != nil {
		return OpenedSession{}, err
	}
	return OpenedSession{Kind: "terminal", Terminal: &info, Fallback: reason}, nil
}

func (a *App) fallbackTerminalWorkspace(wsID, toolID, reason string) (OpenedSession, error) {
	info, err := a.OpenWorkspaceTerminal(wsID, toolID, 0, 0)
	if err != nil {
		return OpenedSession{}, err
	}
	return OpenedSession{Kind: "terminal", Terminal: &info, Fallback: reason}, nil
}

// SendChatPrompt / CancelChat / RespondChatPermission / CancelChatPermission / CloseChat / ListChats / ChatHistory
func (a *App) SendChatPrompt(id, text string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.Prompt(id, text)
}

func (a *App) CancelChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.Cancel(id)
}

func (a *App) RespondChatPermission(id, requestID, optionID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.RespondPermission(id, requestID, optionID)
}

func (a *App) CancelChatPermission(id, requestID string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.CancelPermission(id, requestID)
}

func (a *App) CloseChat(id string) error {
	m := a.chats()
	if m == nil {
		return errNotReady
	}
	return m.Close(id)
}

func (a *App) ListChats() []chat.Info {
	m := a.chats()
	if m == nil {
		return []chat.Info{}
	}
	return m.List()
}

func (a *App) ChatHistory(id string) []chat.Update {
	m := a.chats()
	if m == nil {
		return []chat.Update{}
	}
	h := m.History(id)
	if h == nil {
		return []chat.Update{}
	}
	return h
}

// nextChatSeq 给工作区新建聊天生成唯一 key（每次新建都要新进程）。
func nextChatSeq() string {
	return strconv.FormatInt(chatKeySeq.Add(1), 10)
}

var chatKeySeq atomic.Int64
```

import 仅保留实际用到的：`strconv`、`sync/atomic`，以及 `chat`、`launch`、`launcher`、`providers`、`terminal`（`discovery`/`context` 不需要）。`workspaceTerminalTitle` 已在 `terminal.go` 定义，直接复用；toolID 对应工具未实现 `ACPProvider` 时 `ForWorkspaceACP` 返回 `ErrACPUnavailable`，走回退。

- [ ] **Step 4: 在 app.go 注册依赖**

`Options` 增加：

```go
Chats *chat.Manager
```

`initRealDeps` 在 `Terminals` 之后加：

```go
if a.opts.Chats == nil {
	a.opts.Chats = newChatManager(a)
}
```

`Shutdown` 在关闭 Terminals 后加：

```go
if m := a.snapshot().Chats; m != nil {
	m.CloseAll()
}
```

import 增加 `"github.com/yangk/kshell/internal/chat"`。

- [ ] **Step 5: 跑测试确认通过**

Run: `go vet ./internal/desktop/ ; go test ./internal/desktop/ -run TestChatManager -count=1 -v`
Expected: PASS。

---

## M5：前端

### Task 10: 前端依赖与 api 封装

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: 安装依赖**

Run: `npm install react-markdown remark-gfm`（在 `frontend/`）
Expected: `package.json` dependencies 出现两项。

- [ ] **Step 2: 在 api.ts 增加类型**

```ts
// chat.Info 的 JSON 形态（internal/chat/types.go）
export interface ChatInfo {
  ID: string;
  Kind: string; // session | new
  SessionID: string;
  Workspace: string;
  Title: string;
  ToolID: string;
  Status: string; // starting | ready | running | exited
  ExitCode: number;
  Error: string;
}

export interface ChatToolCall {
  ToolCallID: string;
  Name?: string;
  Title?: string;
  Kind?: string;
  Status?: string;
  Content?: unknown[];
  RawInput?: unknown;
  RawOutput?: unknown;
}

export interface ChatPlanEntry {
  Content: string;
  Priority?: string;
  Status?: string;
}

// chat.Update 的 JSON 形态
export interface ChatUpdate {
  Seq: number;
  Type: string; // user | assistant | thought | tool | plan | turn_done | error
  MessageID?: string;
  Text?: string;
  ToolCallID?: string;
  Tool?: ChatToolCall;
  Plan?: ChatPlanEntry[];
  StopReason?: string;
}

export interface ChatPermissionOption {
  OptionID: string;
  Name: string;
  Kind: string;
}

export interface ChatPermissionRequest {
  RequestID: string;
  SessionID: string;
  ToolCall: ChatToolCall;
  Options: ChatPermissionOption[];
}

// desktop.OpenedSession 的 JSON 形态
export interface OpenedSession {
  Kind: 'chat' | 'terminal';
  Chat?: ChatInfo;
  Terminal?: TerminalInfo;
  Fallback?: string;
}
```

`ToolInfo` 增加：`ACP?: { Available: boolean; Source: string; BinPath?: string; Package?: string }`。

- [ ] **Step 3: 绑定封装 + 事件**

在 `AppBindings` 接口增加：

```ts
OpenSession(sessionID: string): Promise<OpenedSession>;
OpenWorkspace(wsID: string, toolID: string): Promise<OpenedSession>;
SendChatPrompt(id: string, text: string): Promise<void>;
CancelChat(id: string): Promise<void>;
RespondChatPermission(id: string, requestID: string, optionID: string): Promise<void>;
CancelChatPermission(id: string, requestID: string): Promise<void>;
CloseChat(id: string): Promise<void>;
ListChats(): Promise<ChatInfo[]>;
ChatHistory(id: string): Promise<ChatUpdate[]>;
```

在文件末尾增加封装与订阅：

```ts
export async function openSession(sessionID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenSession(sessionID);
}

export async function openWorkspace(wsID: string, toolID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenWorkspace(wsID, toolID);
}

export async function sendChatPrompt(id: string, text: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SendChatPrompt(id, text);
}

export async function cancelChat(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CancelChat(id);
}

export async function respondChatPermission(id: string, requestID: string, optionID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RespondChatPermission(id, requestID, optionID);
}

export async function cancelChatPermission(id: string, requestID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CancelChatPermission(id, requestID);
}

export async function closeChat(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CloseChat(id);
}

export async function listChats(): Promise<ChatInfo[]> {
  const a = app();
  if (!a) return [];
  return a.ListChats();
}

export async function chatHistory(id: string): Promise<ChatUpdate[]> {
  const a = app();
  if (!a) return [];
  return a.ChatHistory(id);
}

export function onChatUpdate(cb: (p: { id: string; update: ChatUpdate }) => void): () => void {
  return EventsOn('chat:update', (p: { id?: string; update?: ChatUpdate }) =>
    cb({ id: p?.id ?? '', update: p?.update as ChatUpdate }),
  );
}

export function onChatPermission(cb: (p: { id: string; request: ChatPermissionRequest }) => void): () => void {
  return EventsOn('chat:permission', (p: { id?: string; request?: ChatPermissionRequest }) =>
    cb({ id: p?.id ?? '', request: p?.request as ChatPermissionRequest }),
  );
}

export function onChatExit(cb: (p: { id: string; exitCode: number; error: string }) => void): () => void {
  return EventsOn('chat:exit', (p: { id?: string; exitCode?: number; error?: string }) =>
    cb({ id: p?.id ?? '', exitCode: p?.exitCode ?? 0, error: p?.error ?? '' }),
  );
}
```

- [ ] **Step 4: 类型检查**

Run: `npm run build`（在 `frontend/`）
Expected: 通过（此时绑定尚未被 UI 使用，tsc 不报错）。

---

### Task 11: store 聊天切片与 reducer

**Files:**
- Create: `frontend/src/state/chatUpdate.ts`
- Create: `frontend/src/state/chatUpdate.test.ts`
- Modify: `frontend/src/state/store.ts`

- [ ] **Step 1: 写失败测试**

```ts
import { describe, expect, it } from 'vitest';
import type { ChatUpdate } from '../lib/api';
import { applyChatUpdate, emptyChatItems } from './chatUpdate';

describe('applyChatUpdate', () => {
  it('按 Seq 去重，累积同 MessageID 的流式文本', () => {
    let items = emptyChatItems();
    items = applyChatUpdate(items, { Seq: 1, Type: 'assistant', MessageID: 'm1', Text: '你' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'assistant', MessageID: 'm1', Text: '好' });
    items = applyChatUpdate(items, { Seq: 2, Type: 'assistant', MessageID: 'm1', Text: 'X' }); // 重复 Seq 忽略
    expect(items).toHaveLength(1);
    expect(items[0].text).toBe('你好');
  });

  it('tool 按 ToolCallID upsert', () => {
    let items = emptyChatItems();
    const u1: ChatUpdate = { Seq: 1, Type: 'tool', ToolCallID: 'c1', Tool: { ToolCallID: 'c1', Title: 'Read', Status: 'pending' } };
    const u2: ChatUpdate = { Seq: 2, Type: 'tool', ToolCallID: 'c1', Tool: { ToolCallID: 'c1', Title: 'Read', Status: 'completed' } };
    items = applyChatUpdate(items, u1);
    items = applyChatUpdate(items, u2);
    expect(items).toHaveLength(1);
    expect(items[0].tool?.Status).toBe('completed');
  });

  it('turn_done 追加事件项', () => {
    const items = applyChatUpdate(emptyChatItems(), { Seq: 1, Type: 'turn_done', StopReason: 'end_turn' });
    expect(items[0].type).toBe('turn_done');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm test -- chatUpdate`（在 `frontend/`）
Expected: 失败（模块不存在）。

- [ ] **Step 3: 实现 chatUpdate.ts**

```ts
import type { ChatPlanEntry, ChatToolCall, ChatUpdate } from '../lib/api';

export interface TimelineItem {
  key: string;
  type: 'user' | 'assistant' | 'thought' | 'tool' | 'plan' | 'turn_done' | 'error';
  text?: string;
  tool?: ChatToolCall;
  plan?: ChatPlanEntry[];
  stopReason?: string;
  seq: number;
}

// 全新的空时间线（与 store 的 chatItems[id] 默认值一致）
export function emptyChatItems(): TimelineItem[] {
  return [];
}

// applyChatUpdate 是纯函数：按 Seq 去重后归并一条更新，返回新数组（不改入参）。
export function applyChatUpdate(items: TimelineItem[], u: ChatUpdate): TimelineItem[] {
  const last = items.length > 0 ? items[items.length - 1].seq : 0;
  if (u.Seq <= last) return items; // 重复/乱序（含 History 与事件重叠）

  if (u.Type === 'assistant' || u.Type === 'thought' || u.Type === 'user') {
    const key = u.MessageID ?? `${u.Type}:${u.Seq}`;
    const idx = items.findIndex((it) => it.key === key && it.type === u.Type);
    if (idx >= 0) {
      const next = items.slice();
      next[idx] = { ...next[idx], text: (next[idx].text ?? '') + (u.Text ?? ''), seq: u.Seq };
      return next;
    }
    return [...items, { key, type: u.Type as TimelineItem['type'], text: u.Text ?? '', seq: u.Seq }];
  }

  if (u.Type === 'tool' && u.Tool) {
    const key = u.ToolCallID ?? u.Tool.ToolCallID;
    const idx = items.findIndex((it) => it.key === key && it.type === 'tool');
    if (idx >= 0) {
      const next = items.slice();
      next[idx] = { ...next[idx], tool: u.Tool, seq: u.Seq };
      return next;
    }
    return [...items, { key, type: 'tool', tool: u.Tool, seq: u.Seq }];
  }

  if (u.Type === 'plan') {
    return [...items, { key: `plan:${u.Seq}`, type: 'plan', plan: u.Plan ?? [], seq: u.Seq }];
  }

  // turn_done / error：作为分界事件追加（不参与文本合并）
  return [...items, {
    key: `${u.Type}:${u.Seq}`,
    type: u.Type as TimelineItem['type'],
    text: u.Text,
    stopReason: u.StopReason,
    seq: u.Seq,
  }];
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npm test -- chatUpdate`
Expected: PASS。

- [ ] **Step 5: 在 store.ts 增加切片**

在 `AppState` 接口与实现中增加：

```ts
chats: ChatInfo[];
upsertChat(info: ChatInfo): void;
removeChat(id: string): void;
markChatExited(id: string, exitCode: number, error: string): void;
setChats(list: ChatInfo[]): void;

chatItems: Record<string, TimelineItem[]>;
chatSeq: Record<string, number>;
applyChat(id: string, u: ChatUpdate): void;
setChatItems(id: string, items: TimelineItem[]): void;
removeChatState(id: string): void;

chatPermissions: Record<string, ChatPermissionRequest | null>;
setChatPermission(id: string, req: ChatPermissionRequest | null): void;
```

实现（`applyChat` 用 `applyChatUpdate` 并记录 seq；`removeChat` 同时清 items/permissions）：

```ts
chats: [],
upsertChat: (info) => set((s) => {
  const idx = s.chats.findIndex((c) => c.ID === info.ID);
  if (idx < 0) return { chats: [...s.chats, info] };
  const chats = s.chats.slice();
  chats[idx] = info;
  return { chats };
}),
removeChat: (id) => set((s) => {
  const { [id]: _i, ...chatItems } = s.chatItems;
  const { [id]: _s, ...chatSeq } = s.chatSeq;
  const { [id]: _p, ...chatPermissions } = s.chatPermissions;
  return { chats: s.chats.filter((c) => c.ID !== id), chatItems, chatSeq, chatPermissions };
}),
markChatExited: (id, exitCode, error) => set((s) => ({
  chats: s.chats.map((c) => (c.ID === id ? { ...c, Status: 'exited', ExitCode: exitCode, Error: error } : c)),
})),
setChats: (list) => set({ chats: list }),

chatItems: {},
chatSeq: {},
applyChat: (id, u) => set((s) => {
  const last = s.chatSeq[id] ?? 0;
  if (u.Seq <= last) return {};
  const items = applyChatUpdate(s.chatItems[id] ?? [], u);
  // 仅负责「一轮结束回到 ready」与错误记录；running 由发送侧乐观置位
  const chats = s.chats.map((c) => {
    if (c.ID !== id) return c;
    if (u.Type === 'error') return { ...c, Status: 'ready', Error: u.Text ?? c.Error };
    if (u.Type === 'turn_done') return { ...c, Status: 'ready' };
    return c;
  });
  return { chatItems: { ...s.chatItems, [id]: items }, chatSeq: { ...s.chatSeq, [id]: u.Seq }, chats };
}),
setChatItems: (id, items) => set((s) => ({
  chatItems: { ...s.chatItems, [id]: items },
  chatSeq: { ...s.chatSeq, [id]: items.length > 0 ? items[items.length - 1].seq : (s.chatSeq[id] ?? 0) },
})),
removeChatState: (id) => set((s) => {
  const { [id]: _i, ...chatItems } = s.chatItems;
  const { [id]: _s, ...chatSeq } = s.chatSeq;
  return { chatItems, chatSeq };
}),

chatPermissions: {},
setChatPermission: (id, req) => set((s) => {
  const next = { ...s.chatPermissions };
  if (req) next[id] = req; else delete next[id];
  return { chatPermissions: next };
}),
```

import 增加 `import type { ChatInfo, ChatPermissionRequest, ChatUpdate } from '../lib/api';` 与 `import { applyChatUpdate, type TimelineItem } from './chatUpdate';`。

- [ ] **Step 6: 跑测试**

Run: `npm test -- chatUpdate ; npm run build`
Expected: 全绿。

---

### Task 12: ChatView 组件

**Files:**
- Create: `frontend/src/components/ChatView.tsx`
- Test: `frontend/src/components/ChatView.test.tsx`

- [ ] **Step 1: 写失败测试**

```tsx
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import ChatView from './ChatView';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  cancelChat: vi.fn(),
  respondChatPermission: vi.fn(),
  cancelChatPermission: vi.fn(),
}));
vi.mock('../lib/api', () => api);

const CHAT: ChatInfo = {
  ID: 'c1', Kind: 'new', SessionID: 's1', Workspace: 'D:\\p', Title: '新会话',
  ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '',
};

beforeEach(() => {
  vi.clearAllMocks();
  api.sendChatPrompt.mockResolvedValue(undefined);
  useAppStore.setState({ chatItems: {}, chatSeq: {}, chatPermissions: {}, chats: [CHAT] });
});
afterEach(cleanup);

describe('ChatView', () => {
  it('输入并发送调用 sendChatPrompt', () => {
    render(<ChatView chat={CHAT} active />);
    const box = screen.getByPlaceholderText(/输入/);
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(api.sendChatPrompt).toHaveBeenCalledWith('c1', 'hello');
  });

  it('running 时显示停止并调用 cancelChat', () => {
    render(<ChatView chat={{ ...CHAT, Status: 'running' }} active />);
    fireEvent.click(screen.getByRole('button', { name: /停止/ }));
    expect(api.cancelChat).toHaveBeenCalledWith('c1');
  });

  it('渲染时间线与权限弹窗', () => {
    useAppStore.setState({
      chatItems: { c1: [
        { key: 'm1', type: 'user', text: 'hi', seq: 1 },
        { key: 'm2', type: 'assistant', text: '**好的**', seq: 2 },
      ] },
      chatPermissions: { c1: { RequestID: 'r1', SessionID: 's1', ToolCall: { ToolCallID: 'c1' }, Options: [{ OptionID: 'allow', Name: '允许', Kind: 'allow_once' }] } },
    });
    render(<ChatView chat={CHAT} active />);
    expect(screen.getByText('hi')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    expect(api.respondChatPermission).toHaveBeenCalledWith('c1', 'r1', 'allow');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm test -- ChatView`
Expected: 失败（组件不存在）。

- [ ] **Step 3: 实现 ChatView.tsx**

```tsx
// 中心区 ACP 聊天页签：时间线 + 输入区 + 权限弹窗。常挂载、由父级 hidden 切换。
import { useEffect, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { ChatInfo } from '../lib/api';
import { cancelChat, cancelChatPermission, respondChatPermission, sendChatPrompt } from '../lib/api';
import { useAppStore } from '../state/store';
import { cn } from '../lib/cn';

interface Props {
  chat: ChatInfo;
  active: boolean;
}

export default function ChatView({ chat, active }: Props) {
  const id = chat.ID;
  const items = useAppStore((s) => s.chatItems[id]) ?? [];
  const permission = useAppStore((s) => s.chatPermissions[id]) ?? null;
  const [draft, setDraft] = useState('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const running = chat.Status === 'running';

  useEffect(() => {
    if (!active) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [items, active]);

  const send = () => {
    const text = draft.trim();
    if (!text || running) return;
    setDraft('');
    // 乐观置 running：turn_done/error 的 chat:update 会把它改回 ready
    useAppStore.getState().upsertChat({ ...chat, Status: 'running' });
    void sendChatPrompt(id, text);
  };

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      {chat.Status === 'exited' && (
        <div className="shrink-0 bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          会话已退出{chat.ExitCode ? `（退出码 ${chat.ExitCode}）` : ''}{chat.Error ? `：${chat.Error}` : ''}
        </div>
      )}
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-2">
        {items.map((it) => {
          if (it.type === 'user' || it.type === 'assistant' || it.type === 'thought') {
            return (
              <div key={it.key} className={cn('mb-3', it.type === 'user' ? 'text-right' : '')}>
                <div className={cn('inline-block max-w-[85%] rounded-md px-3 py-1.5 text-sm',
                  it.type === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted',
                  it.type === 'thought' && 'italic text-muted-foreground')}>
                  {it.type === 'assistant'
                    ? <ReactMarkdown remarkPlugins={[remarkGfm]}>{it.text ?? ''}</ReactMarkdown>
                    : <span className="whitespace-pre-wrap">{it.text}</span>}
                </div>
              </div>
            );
          }
          if (it.type === 'tool' && it.tool) {
            return (
              <div key={it.key} className="mb-2 rounded border border-border px-2 py-1 text-xs">
                <span className="font-medium">{it.tool.Title || it.tool.Name || '工具'}</span>
                <span className="ml-2 text-muted-foreground">{it.tool.Kind}{it.tool.Status ? ` · ${it.tool.Status}` : ''}</span>
              </div>
            );
          }
          if (it.type === 'plan') {
            return (
              <ul key={it.key} className="mb-2 list-disc pl-5 text-xs text-muted-foreground">
                {(it.plan ?? []).map((e, i) => <li key={i}>{e.Content}</li>)}
              </ul>
            );
          }
          if (it.type === 'error') {
            return <div key={it.key} className="mb-2 text-xs text-destructive">错误：{it.text}</div>;
          }
          return null;
        })}
      </div>

      <div className="shrink-0 border-t border-border p-2">
        <textarea
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder="输入消息，Enter 发送，Shift+Enter 换行"
          value={draft}
          disabled={chat.Status !== 'ready' && chat.Status !== 'running'}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
          }}
        />
        <div className="mt-1 flex justify-end gap-2">
          {running
            ? <button className="rounded bg-muted px-3 py-1 text-xs" onClick={() => void cancelChat(id)}>停止</button>
            : <button className="rounded bg-primary px-3 py-1 text-xs text-primary-foreground" onClick={send}>发送</button>}
        </div>
      </div>

      {permission && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/30">
          <div className="w-96 rounded-md bg-card p-4 shadow-lg">
            <p className="mb-1 text-sm font-medium">请求权限</p>
            <p className="mb-3 text-xs text-muted-foreground">{permission.ToolCall.Title || permission.ToolCall.Name || '工具调用'}</p>
            <div className="flex flex-col gap-2">
              {permission.Options.map((o) => (
                <button key={o.OptionID} className="rounded border border-border px-3 py-1.5 text-sm"
                  onClick={() => void respondChatPermission(id, permission.RequestID, o.OptionID)}>
                  {o.Name}
                </button>
              ))}
              <button className="rounded px-3 py-1.5 text-xs text-muted-foreground"
                onClick={() => void cancelChatPermission(id, permission.RequestID)}>拒绝</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npm test -- ChatView`
Expected: PASS。

---

### Task 13: 页签与事件集成（WorkspaceTab / App）

**Files:**
- Modify: `frontend/src/pages/WorkspaceTab.tsx`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/components/WorkspaceTab.test.tsx`（扩展）

- [ ] **Step 1: 写失败测试（WorkspaceTab：ACP 可用走聊天，不可用回退终端）**

在 `frontend/src/components/WorkspaceTab.test.tsx` 增加（复用该文件既有的 render + api mock 方式）：

```tsx
it('openWorkspace 返回 chat 时中心区出现聊天页签', async () => {
  const api = await import('../lib/api');
  vi.spyOn(api, 'openWorkspace').mockResolvedValue({
    Kind: 'chat',
    Chat: { ID: 'c1', Kind: 'new', SessionID: 's1', Workspace: 'D:\\p', Title: '新会话 · Claude Code', ToolID: 'claude', Status: 'ready', ExitCode: 0, Error: '' },
  });
  render(<WorkspaceTabView tab={{ id: 'D:\\p', name: 'p' }} visible />);
  // 触发「新建会话」并选中 Claude（沿用本文件既有触发方式）
  // 断言：openWorkspace 被调用（而非 openWorkspaceTerminal），且中心区 tablist 含 '新会话 · Claude Code'
  await waitFor(() => expect(api.openWorkspace).toHaveBeenCalled());
  expect(await screen.findByText('新会话 · Claude Code')).toBeInTheDocument();
});
```

要点：`openWorkspace` 桩返回 `Kind:'chat'` 后，中心区 tablist 必须出现该标题；不得再调用 `openWorkspaceTerminal`。

- [ ] **Step 2: 跑测试确认失败**

Run: `npm test -- WorkspaceTab`
Expected: 新增用例失败（`openWorkspace` 未被调用 / 无聊天页签）。

- [ ] **Step 3: 修改 WorkspaceTab.tsx**

- 引入：`openWorkspace, openSession, closeChat, listChats, chatHistory, onChatUpdate` 等；引入 `ChatView`、`OpenedSession`、`ChatInfo`。
- 从 store 取 `chats`、`chatItems`、`upsertChat`、`removeChat`、`applyChat`、`setChatItems`。
- 本工作区聊天：`const chats = useMemo(() => chatList.filter(c => sameWorkspacePath(c.Workspace, tab.id)), [...])`。
- `openSessionTerminal(s)` → `openChatOrTerminal(s.ID)`：调用 `openSession(id)`，按 `res.Kind` 入 store（chat → `upsertChat(res.Chat)`；terminal → `upsertTerminal(res.Terminal)`），`setCenterTab(id)`；`res.Fallback` 非空时 `notify('已回退到终端模式：' + res.Fallback, 'info')`。
- `startSession(id)` → `openWorkspace(tab.id, id)`，同上（chat 的新会话沿用现有延迟重扫逻辑）。
- 中心区页签条：`chats` 追加渲染聊天页签（`ToolDot` + `t.Title` + 运行中指示 + `×` → `handleCloseChat`）。
- 内容区：为每个 chat 渲染 `<ChatView chat={c} active={visible && centerTab === c.ID} />`（`hidden` 非激活）。
- `handleCloseChat(id)`：`removeChat(id)` + `closeChat(id)`；`centerTab===id` 退回预览。
- useEffect：`centerTab` 指向的 chat 不存在时退回预览（与终端一致）。

- [ ] **Step 4: 修改 App.tsx**

- 挂载时：`listChats().then(list => { setChats(list); list.forEach(c => chatHistory(c.ID).then(h => h.forEach(u => applyChat(c.ID, u)))); })`（`applyChat` 已按 Seq 去重，History 与实时事件重叠安全）。
- 全局订阅一次：

```tsx
const offU = onChatUpdate(({ id, update }) => useAppStore.getState().applyChat(id, update));
const offP = onChatPermission(({ id, request }) => useAppStore.getState().setChatPermission(id, request));
const offX = onChatExit(({ id, exitCode, error }) => {
  const { markChatExited, setChatPermission, notify } = useAppStore.getState();
  markChatExited(id, exitCode, error);
  setChatPermission(id, null);
  notify(error || `会话已退出（退出码 ${exitCode}）`, error ? 'error' : 'info');
});
return () => { offU(); offP(); offX(); };
```

- `handleCloseTab`：连带关闭该工作区的聊天：

```tsx
const { chats, removeChat } = useAppStore.getState();
for (const c of chats.filter(c => sameWorkspacePath(c.Workspace, id))) {
  removeChat(c.ID);
  closeChat(c.ID).catch(() => {});
}
```

- [ ] **Step 5: 跑前端全量测试与构建**

Run: `npm test ; npm run build`（在 `frontend/`）
Expected: 全绿。

---

## M6：验证与冒烟

### Task 14: Go 全量验证

- [ ] **Step 1: 跑 Go 侧**

Run: `go build ./... ; go vet ./... ; go test ./... -count=1`
Expected: 全绿。若有既有测试因 `Tool`/`Options` 结构变化而失败，一并修正。

- [ ] **Step 2: 前端全量**

Run: `npm test ; npm run build`（在 `frontend/`）
Expected: 全绿。

---

### Task 15: 真机冒烟与文档

**Files:**
- Modify: `docs/smoke-test*.md`（按现有文件追加）

- [ ] **Step 1: 验证风险 1（关键前提）**

在有 Claude 登录态与 Node ≥ 22 的机器上：
1. 确认 `claude-agent-acp` 或 `npx @agentclientprotocol/claude-agent-acp` 可启动；
2. 记录一个真实存在的 `~/.claude/projects/<slug>/<uuid>.jsonl` 的文件名 `uuid`；
3. 桌面端对该会话点「恢复」，观察是否走 `session/load{uuid}` 并重放出历史。
Expected: 历史正确重放则前提成立；否则在冒烟文档记录结论，并回到设计 §9 调整「恢复历史」方案（例如回退 TUI）。

- [ ] **Step 2: 跑冒烟清单**

在 `docs/smoke-test*.md` 追加条目并逐项验证：
- 工作区新建会话 → 出现聊天页签，发送消息有流式回复；
- 恢复历史会话 → 聊天页签重放历史；
- 触发工具调用 → 出现工具卡片；触发权限请求 → 弹窗选择后继续；
- 运行中点「停止」→ 本轮 cancelled；
- 关闭聊天页签 → 进程结束；
- 卸载/改名 `claude-agent-acp` 且无 npx → 入口自动回退 TUI，无报错；
- 重载前端（开发态）→ 仍运行的聊天页签与时间线恢复。

- [ ] **Step 3: 构建可运行程序（收尾，需在主工作区/主干执行）**

Run: `.\build.ps1 -Desktop`（仓库根）
Expected: 产出 `dist\kshell-desktop.exe`。

---

## 自查记录

- 规格覆盖：§3 分层 → Task 1-5/9；§5 provider/discovery/launch → Task 6-8；§7 绑定/统一入口 → Task 9；§8 前端 → Task 10-13；§10 错误与边界 → Task 3（失败/关闭/未知方法）、Task 5（pending 取消）、Task 9（回退）；§11 测试 → 各任务内；§12 风险 → Task 15。
- 类型一致性：`chat.Update` 字段（Seq/Type/MessageID/Text/ToolCallID/Tool/Plan/StopReason）在 Go、TS、store reducer、ChatView 一致；`Conn` 方法在 `backend.go`、`fakeConn`、`acp.Agent` 一致。
- 已知实现注意：Task 3 `failPending` 必须向 pending channel 发送显式错误 message（而非 close），避免 `call` 读到零值误判成功。
