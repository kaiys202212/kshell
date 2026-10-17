package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/yangk/kshell/internal/executil"
)

// Logf 是宿主注入的日志钩子（desktop 侧重定向到 ~/.kshell/logs/desktop.log）。
// 协议层自身零依赖：nil（默认，含测试）时静默。
var Logf func(format string, args ...any)

func logf(format string, args ...any) {
	if Logf != nil {
		Logf(format, args...)
	}
}

// StderrTail 取 stderr 缓冲的尾部 ≤limit 字节（从最近的行首起，避免半行）。
// 导出供 chat 层在异常停止时归因使用（上游 CLI 把 API 错误细节只打到 stderr）。
func StderrTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	tail := s[len(s)-limit:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		tail = tail[i+1:]
	}
	return tail
}

// Handler 接收 agent 主动发来的通知与反向请求。
// SessionUpdate 在读循环内同步调用（保证与响应/重放的顺序）；
// RequestPermission 在独立 goroutine 中调用，可阻塞等待用户。
type Handler interface {
	SessionUpdate(sessionID string, update json.RawMessage)
	RequestPermission(ctx context.Context, sessionID, requestID string, p RequestPermissionParams) (PermissionOutcome, error)
}

// syncBuffer 是带锁的 bytes.Buffer：os/exec 的 stderr 拷贝 goroutine 写入，
// 而 Stderr() 可能在任意时刻读取，必须互斥避免数据竞争。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Agent 是一个已启动的 ACP agent 连接（一个子进程）。
type Agent struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	codec   *codec
	stderr  *syncBuffer
	handler Handler

	writeMu sync.Mutex

	mu       sync.Mutex
	nextID   int64
	pending  map[string]chan message
	closed   bool
	exitCode int
	waitErr  error

	baseCtx    context.Context // 反向请求处理器的基础 ctx，Close/进程退出时取消
	baseCancel context.CancelFunc

	mcp []McpServerStdio

	settingsMeta json.RawMessage // session/new、session/load 的 _meta 载荷

	readDone  chan struct{} // 读循环结束
	done      chan struct{} // 连接关闭（读循环结束或 Close）
	waitDone  chan struct{} // 子进程退出
	closeOnce sync.Once
}

// NewAgent 启动子进程并返回连接；调用方负责 Close。
func NewAgent(ctx context.Context, spec Spec, h Handler) (*Agent, error) {
	if spec.Path == "" {
		return nil, errors.New("err.acp.no_agent")
	}
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	executil.HideWindow(cmd)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	sb := &syncBuffer{}
	cmd.Stderr = sb
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("err.acp.start_failed|%w", err)
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	a := &Agent{
		cmd:        cmd,
		stdin:      stdin,
		codec:      newCodec(bufio.NewReaderSize(stdout, 1<<20), stdin),
		stderr:     sb,
		handler:    h,
		pending:    make(map[string]chan message),
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
		readDone:   make(chan struct{}),
		done:       make(chan struct{}),
		waitDone:   make(chan struct{}),
	}
	go a.readLoop()
	go a.waitLoop()
	return a, nil
}

func (a *Agent) readLoop() {
	defer close(a.readDone)
	for {
		m, err := a.codec.read()
		if err != nil {
			if !errors.Is(err, io.EOF) { // 进程退出的正常 EOF 不打，避免与退出日志重复
				logf("acp: 读循环异常结束 err=%v", err)
			}
			a.failPending(err)
			return
		}
		a.dispatch(m)
	}
}

// dispatch 分流：响应投递等待者；通知同步处理；反向请求独立 goroutine 处理。
func (a *Agent) dispatch(m message) {
	switch {
	case m.Method == "" && m.ID != nil: // 响应
		a.mu.Lock()
		ch := a.pending[string(*m.ID)]
		delete(a.pending, string(*m.ID))
		a.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	case m.Method == "": // 无 method 无 id：忽略
	case m.ID == nil: // 通知：同步处理，保证与后续响应的顺序
		if m.Method == "session/update" && a.handler != nil {
			sid, upd, err := decodeUpdateParams(m.Params)
			if err == nil {
				a.handler.SessionUpdate(sid, upd)
			}
		}
	default: // 反向请求
		go a.handleRequest(m)
	}
}

func (a *Agent) handleRequest(m message) {
	// 应答 id 原样回传：JSON-RPC 2.0 要求响应 id 与请求 id 值且类型一致，
	// 数字 id 被字符串化后，严格路由的真实 agent（如 codebuddy）会丢弃应答，
	// 反向请求永远等不到回复 → session/prompt 挂死。
	id := m.RawID
	if len(id) == 0 {
		id, _ = json.Marshal(string(*m.ID))
	}
	normID := string(*m.ID)
	if m.Method == "session/request_permission" && a.handler != nil {
		p, err := decodeRequestPermissionParams(m.Params)
		if err != nil {
			a.writeResponse(id, nil, &RPCError{Code: -32602, Message: "invalid params"})
			return
		}
		out, err := a.handler.RequestPermission(a.baseCtx, p.SessionID, normID, p)
		if err != nil {
			a.writeResponse(id, nil, &RPCError{Code: -32603, Message: err.Error()})
			return
		}
		a.writeResponse(id, RequestPermissionResult{Outcome: out}, nil)
		return
	}
	a.writeResponse(id, nil, &RPCError{Code: -32601, Message: "method not found: " + m.Method})
}

func (a *Agent) writeResponse(id json.RawMessage, result any, rpcErr *RPCError) {
	env := responseEnvelope{JSONRPC: jsonrpcVersion, ID: id, Result: result, Error: rpcErr}
	if result == nil && rpcErr == nil {
		env.Result = struct{}{}
	}
	a.writeMu.Lock()
	_ = a.codec.write(env)
	a.writeMu.Unlock()
}

func (a *Agent) call(ctx context.Context, method string, params, out any) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("err.acp.closed")
	}
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

// SetMCP 设置随后 session/new 与 session/load 要带上的 MCP 服务器。
func (a *Agent) SetMCP(servers []McpServerStdio) {
	a.mu.Lock()
	a.mcp = append([]McpServerStdio(nil), servers...)
	a.mu.Unlock()
}

// SetSettingsMeta 设置 session/new 与 session/load 参数里的 _meta 载荷
//（如 claude 的 {"claudeCode":{"options":{"settings":"..."}}}）。
// 空串表示不携带。签名用 string：chat 层的可选接口断言按精确签名匹配，
// json.RawMessage 形参会让 *Agent 不满足 interface{ SetSettingsMeta(string) }。
func (a *Agent) SetSettingsMeta(raw string) {
	a.mu.Lock()
	a.settingsMeta = json.RawMessage(raw)
	a.mu.Unlock()
}

func (a *Agent) settingsMetaParams() json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settingsMeta
}

func (a *Agent) mcpParams() []any {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.mcp) == 0 {
		return []any{}
	}
	out := make([]any, len(a.mcp))
	for i := range a.mcp {
		s := a.mcp[i]
		if s.Args == nil {
			s.Args = []string{}
		}
		if s.Env == nil {
			s.Env = []EnvVariable{}
		}
		out[i] = s
	}
	return out
}

// sessionStartFromResult 从 new/load 响应提取调用方需要的会话信息。
func sessionStartFromResult(res NewSessionResult) SessionStart {
	out := SessionStart{SessionID: res.SessionID, Models: res.Models}
	for i := range res.ConfigOptions {
		if res.ConfigOptions[i].ID == "model" {
			opt := res.ConfigOptions[i]
			out.ModelConfig = &opt
			break
		}
	}
	return out
}

func (a *Agent) NewSession(ctx context.Context, cwd string) (SessionStart, error) {
	var res NewSessionResult
	err := a.call(ctx, "session/new", NewSessionParams{Cwd: cwd, McpServers: a.mcpParams(), Meta: a.settingsMetaParams()}, &res)
	if err != nil {
		return SessionStart{}, err
	}
	return sessionStartFromResult(res), nil
}

func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string) (SessionStart, error) {
	var res NewSessionResult
	if err := a.call(ctx, "session/load", LoadSessionParams{SessionID: sessionID, Cwd: cwd, McpServers: a.mcpParams(), Meta: a.settingsMetaParams()}, &res); err != nil {
		return SessionStart{}, err
	}
	return sessionStartFromResult(res), nil
}

func (a *Agent) Prompt(ctx context.Context, sessionID string, blocks []ContentBlock) (string, error) {
	var res PromptResult
	params := PromptParams{SessionID: sessionID, Prompt: blocks}
	if err := a.call(ctx, "session/prompt", params, &res); err != nil {
		return "", err
	}
	return res.StopReason, nil
}

// SetModel 运行时切换会话模型：优先 session/set_model（v1 形态）；
// agent 返回 -32601（method not found）时回退新式的 session/set_config_option
//（optionId 固定 "model"，当前官方适配器即此形态）。两条路都不通时错误上抛。
func (a *Agent) SetModel(ctx context.Context, sessionID, modelID string) error {
	err := a.call(ctx, "session/set_model", SetModelParams{SessionID: sessionID, ModelID: modelID}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32601 {
		return err
	}
	value, err := json.Marshal(modelID)
	if err != nil {
		return err
	}
	return a.call(ctx, "session/set_config_option", SetConfigOptionParams{SessionID: sessionID, OptionID: "model", Value: value}, nil)
}

func (a *Agent) Cancel(sessionID string) error {
	return a.notify("session/cancel", CancelParams{SessionID: sessionID})
}

func (a *Agent) Stderr() string { return a.stderr.String() }

func (a *Agent) waitLoop() {
	// 必须等 stdout 读完再 Wait：否则 os/exec 可能提前关闭管道丢弃尾部输出。
	<-a.readDone
	err := a.cmd.Wait()
	a.mu.Lock()
	a.waitErr = err
	if a.cmd.ProcessState != nil {
		a.exitCode = a.cmd.ProcessState.ExitCode()
	}
	code := a.exitCode
	a.mu.Unlock()
	// agent 为什么停了，唯一线索就是退出码与 stderr：必须落盘（此前 Stderr() 无调用点）。
	logf("acp: agent 退出 code=%d err=%v", code, err)
	if tail := StderrTail(a.stderr.String(), 2048); strings.TrimSpace(tail) != "" {
		logf("acp: agent stderr 尾部: %s", tail)
	}
	close(a.waitDone)
	a.failPending(err) // 进程退出后读循环可能仍阻塞：主动收尾
}

// failPending 关闭连接并唤醒所有等待者（投递显式错误，避免零值被误判为成功）。
func (a *Agent) failPending(err error) {
	if err == nil {
		err = io.EOF
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	for id, ch := range a.pending {
		delete(a.pending, id)
		ch <- message{Error: &RPCError{Code: -32000, Message: err.Error()}}
	}
	a.mu.Unlock()
	a.baseCancel() // 取消在途的反向请求处理器，避免泄漏阻塞
	close(a.done)
}

// Wait 等子进程退出并返回退出码；重复调用安全。
func (a *Agent) Wait() (int, error) {
	<-a.waitDone
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitCode, a.waitErr
}

// Close 结束连接：关 stdin、杀进程并唤醒等待者，幂等。
func (a *Agent) Close() error {
	a.closeOnce.Do(func() {
		_ = a.stdin.Close()
		killProcessTree(a.cmd)
	})
	a.failPending(io.EOF)
	return nil
}
