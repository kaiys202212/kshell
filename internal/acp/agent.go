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
	"sync"

	"github.com/yangk/kshell/internal/executil"
)

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

	writeErrMu   sync.Mutex
	lastWriteErr error

	readDone  chan struct{} // 读循环结束
	done      chan struct{} // 连接关闭（读循环结束或 Close）
	waitDone  chan struct{} // 子进程退出
	closeOnce sync.Once
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
		return nil, fmt.Errorf("启动 ACP agent 失败: %w", err)
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
	if m.Method == "session/request_permission" && a.handler != nil {
		id := string(*m.ID)
		p, err := decodeRequestPermissionParams(m.Params)
		if err != nil {
			a.writeResponse(id, nil, &RPCError{Code: -32602, Message: "invalid params"})
			return
		}
		out, err := a.handler.RequestPermission(a.baseCtx, p.SessionID, id, p)
		if err != nil {
			a.writeResponse(id, nil, &RPCError{Code: -32603, Message: err.Error()})
			return
		}
		a.writeResponse(id, RequestPermissionResult{Outcome: out}, nil)
		return
	}
	a.writeResponse(string(*m.ID), nil, &RPCError{Code: -32601, Message: "method not found: " + m.Method})
}

func (a *Agent) writeResponse(id string, result any, rpcErr *RPCError) {
	env := responseEnvelope{JSONRPC: jsonrpcVersion, ID: id, Result: result, Error: rpcErr}
	if result == nil && rpcErr == nil {
		env.Result = struct{}{}
	}
	a.writeMu.Lock()
	err := a.codec.write(env)
	a.writeMu.Unlock()
	if err != nil {
		a.writeErrMu.Lock()
		a.lastWriteErr = err
		a.writeErrMu.Unlock()
	}
}

func (a *Agent) call(ctx context.Context, method string, params, out any) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("acp: 连接已关闭")
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

func (a *Agent) NewSession(ctx context.Context, cwd string) (string, error) {
	var res NewSessionResult
	err := a.call(ctx, "session/new", NewSessionParams{Cwd: cwd, McpServers: []any{}}, &res)
	return res.SessionID, err
}

func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string) error {
	var res NewSessionResult
	return a.call(ctx, "session/load", LoadSessionParams{SessionID: sessionID, Cwd: cwd, McpServers: []any{}}, &res)
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
	// 必须等 stdout 读完再 Wait：否则 os/exec 可能提前关闭管道丢弃尾部输出。
	<-a.readDone
	err := a.cmd.Wait()
	a.mu.Lock()
	a.waitErr = err
	if a.cmd.ProcessState != nil {
		a.exitCode = a.cmd.ProcessState.ExitCode()
	}
	a.mu.Unlock()
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
