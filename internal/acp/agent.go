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
	"sync/atomic"
	"time"

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

// ElicitationHandler 是可选能力：实现了它的 Handler 才会收到 elicitation/create
// 表单请求。独立接口避免给所有既有实现增加必选方法。
// requestID 是 JSON-RPC 请求 id；ElicitationParams.RequestID 则是 params 里
// 单独的 requestId 字段（用于请求范围内的关联），两者不要混淆。
type ElicitationHandler interface {
	Elicitation(ctx context.Context, requestID string, p ElicitationParams) (ElicitationResult, error)
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

// Len 返回当前缓冲字节数：供上层按轮次取 stderr 增量，避免整段复制与旧错误串轮。
func (s *syncBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

// Since 返回 mark 之后的增量与当前总长度；同一次加锁取齐，避免与并发写入错位。
func (s *syncBuffer) Since(mark int) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	full := s.b.String()
	if mark < 0 || mark > len(full) {
		return full, len(full)
	}
	return full[mark:], len(full)
}

// Agent 是一个已启动的 ACP agent 连接（一个子进程）。
type Agent struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser // os.Pipe 读端：宽限期后可强制关闭，避免读循环永久悬挂
	codec   *codec
	stderr  *syncBuffer
	handler Handler
	label   string // 退出日志归属（聊天ID/工具ID），启动后不可变

	// deliberate 标记 Close() 主动关闭：Windows 上杀进程的退出码 1 与真实
	// 崩溃同形，waitLoop 依据它区分「主动关闭」与「异常退出」。
	deliberate atomic.Bool

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
	// 与探测进程同款约束：绝不能依赖 CommandContext 的超时杀——Windows 上
	// agent 经 npm .cmd 包装启动，ctx 取消只杀直属 cmd.exe，node 真身成孤儿。
	// 因此这里传 Background（无 Done，不会触发 CommandContext 杀进程），
	// 只用统一入口做 shim 解析 / 隐藏黑窗 / 批处理包装；收尾由下方独立守卫
	// goroutine 做进程组树杀兜底。
	cmd := executil.NewCLICommand(context.Background(), spec.Path, spec.Args)
	// 独立进程组：Close/ctx 取消时组杀整棵 ACP 进程树（npx → 适配器 node）。
	executil.PrepareProcessGroup(cmd)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// 显式 os.Pipe 作 stdout（而非 cmd.StdoutPipe）：exec.Wait 不认识这对管道，
	// 进程退出时不会因孙进程仍持有写端而让读端永久阻塞在 Read 上；宽限期后
	// waitLoop 可主动关读端收尾。StdoutPipe + 「先等 EOF 再 Wait」正是
	// 「进程已死但 watchExit 永不返回」的根因。
	stdout, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	sb := &syncBuffer{}
	cmd.Stderr = sb
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("err.acp.start_failed|%w", err)
	}
	// 父进程只保留读端：写端已复制给子进程（及继承它的孙进程）。
	_ = pw.Close()
	baseCtx, baseCancel := context.WithCancel(context.Background())
	a := &Agent{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		codec:      newCodec(bufio.NewReaderSize(stdout, 1<<20), stdin),
		stderr:     sb,
		handler:    h,
		label:      spec.Label,
		pending:    make(map[string]chan message),
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
		readDone:   make(chan struct{}),
		done:       make(chan struct{}),
		waitDone:   make(chan struct{}),
	}
	go a.readLoop()
	go a.waitLoop()
	go func() {
		select {
		case <-ctx.Done(): // 调用方取消：树杀整棵进程（正常关闭走 Close，不经此路）
			executil.KillProcessTree(cmd)
		case <-a.done:
		}
	}()
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
	// elicitation/create 分支放在权限分支之前，仅为可读性：先处理表单类反向请求，
	// 再处理权限确认。
	if m.Method == "elicitation/create" {
		var p ElicitationParams
		// 缺省 mode 按 form 处理：ACP 允许 agent 省略 mode，不能因此把表单
		// 当成不支持而直接 cancel（用户永远看不到表单）。
		if err := json.Unmarshal(m.Params, &p); err != nil || (p.Mode != "" && p.Mode != "form") {
			a.writeResponse(id, ElicitationResult{Action: "cancel"}, nil)
			return
		}
		eh, ok := a.handler.(ElicitationHandler)
		if !ok {
			a.writeResponse(id, ElicitationResult{Action: "cancel"}, nil)
			return
		}
		res, err := eh.Elicitation(a.baseCtx, normID, p)
		// action 只接受规范三值；非 accept 时清空 content，避免把已填数据
		// 误传给放弃/取消的请求。
		switch res.Action {
		case "accept":
		case "decline", "cancel":
			res.Content = nil
		default:
			res = ElicitationResult{Action: "cancel"}
		}
		if err != nil {
			res = ElicitationResult{Action: "cancel"}
		}
		a.writeResponse(id, res, nil)
		return
	}
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
		// 只声明 elicitation.form：kshell 能渲染结构化表单，其余可选能力不虚报。
		ClientCapabilities: ClientCapabilities{
			Elicitation: &ElicitationCapabilities{Form: &struct{}{}},
		},
		ClientInfo: Implementation{Name: "kshell", Title: "Capybara", Version: "0.1.0"},
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
// （如 claude 的 {"claudeCode":{"options":{"settings":{...}}}}，载荷内部
// 形态由 launch 层负责）。空串表示不携带。签名用 string：chat 层的可选
// 接口断言按精确签名匹配，json.RawMessage 形参会让 *Agent 不满足
// interface{ SetSettingsMeta(string) }。
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
	out := SessionStart{SessionID: res.SessionID, Models: res.Models, ConfigOptions: res.ConfigOptions}
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
// （configId 固定 "model"，当前官方适配器即此形态）。两条路都不通时错误上抛。
func (a *Agent) SetModel(ctx context.Context, sessionID, modelID string) error {
	err := a.call(ctx, "session/set_model", SetModelParams{SessionID: sessionID, ModelID: modelID}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32601 {
		return err
	}
	_, err = a.SetConfigOption(ctx, sessionID, "model", modelID)
	return err
}

// SetConfigOption 设置会话配置项（model / mode 等），返回 agent 报告的最新
// 完整配置状态。value 是 select 选项的字符串值（布尔型配置项当前不声明，
// 因此不接受非字符串值）。
func (a *Agent) SetConfigOption(ctx context.Context, sessionID, configID, value string) ([]SessionConfigOption, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var res SetConfigOptionResult
	if err := a.call(ctx, "session/set_config_option", SetConfigOptionParams{SessionID: sessionID, ConfigID: configID, Value: raw}, &res); err != nil {
		return nil, err
	}
	return res.ConfigOptions, nil
}

// Authenticate 用指定登录方式完成认证：未登录的 session/new|load 报
// -32000 authRequired 后，客户端选 initialize 广播的 authMethod 调本方法，
// agent 端负责拉起官方登录流程（如 Cursor 的浏览器登录）。out 恒为 nil
// （规范只回空结果），错误上抛由 chat 层归一化。
func (a *Agent) Authenticate(ctx context.Context, methodID string) error {
	return a.call(ctx, "authenticate", AuthenticateParams{MethodID: methodID}, nil)
}

func (a *Agent) Cancel(sessionID string) error {
	return a.notify("session/cancel", CancelParams{SessionID: sessionID})
}

func (a *Agent) Stderr() string { return a.stderr.String() }

// StderrLen 返回 stderr 累计字节数（O(1)）：上层按轮次记录水位后只取增量。
func (a *Agent) StderrLen() int { return a.stderr.Len() }

// StderrSince 返回 mark 之后的 stderr 增量与当前总长度（同一次加锁取齐）。
func (a *Agent) StderrSince(mark int) (string, int) { return a.stderr.Since(mark) }

// Alive 报告连接是否仍可发起调用：读循环结束/主动 Close 后为 false。
// 进程退出的 Wait 是独立 goroutine，单靠 exited 标志会留下定时器早于退出标记
// 触发的窗口；自动续跑在调度与触发时都应先问 Alive。
func (a *Agent) Alive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed
}

// pipeDrainGrace 是进程退出后等待 stdout 读尽的宽限期：正常路径下读循环
// 立即 EOF；孙进程仍持有写端时，超过宽限期强制关读端收尾（丢的只能是
// 残余输出，不能因此让 watchExit 永久阻塞）。
var pipeDrainGrace = 500 * time.Millisecond

func (a *Agent) waitLoop() {
	// Wait 与读循环解耦：进程退出（Wait 返回）是退出检测的唯一权威信号，
	// 不能把它押在 stdout EOF 上——npx→node 或 MCP 孙进程会继承写端。
	waitCh := make(chan error, 1)
	go func() { waitCh <- a.cmd.Wait() }()
	var err error
	select {
	case err = <-waitCh:
		select {
		case <-a.readDone: // 正常：尾部输出已读尽
		case <-time.After(pipeDrainGrace):
			logf("acp: stdout 未在宽限期内关闭 label=%s（孙进程持有管道？），强制收尾", a.label)
			if a.stdout != nil {
				_ = a.stdout.Close()
			}
		}
	case <-a.readDone:
		// 读循环先结束（EOF/错误）：等进程退出，保持既有语义。
		err = <-waitCh
	}
	a.mu.Lock()
	a.waitErr = err
	if a.cmd.ProcessState != nil {
		a.exitCode = a.cmd.ProcessState.ExitCode()
	}
	code := a.exitCode
	a.mu.Unlock()
	// agent 为什么停了，唯一线索就是退出码与 stderr：必须落盘（此前 Stderr() 无调用点）。
	// 带 label（聊天ID/工具ID）并区分主动关闭/异常退出，否则 desktop.log 无法归因。
	reason := "异常退出"
	if a.deliberate.Load() {
		reason = "主动关闭"
	}
	logf("acp: agent %s label=%s code=%d err=%v", reason, a.label, code, err)
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
		// -32603（JSON-RPC internal error）：连接断开不是 agent 的业务语义，
		// 不得占用规范保留给 authRequired 的 -32000，否则上层会把进程崩溃误判成未登录。
		ch <- message{Error: &RPCError{Code: -32603, Message: err.Error()}}
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
		// 先置位再杀进程：waitLoop 在 Wait 返回后读该标志，若后置会有竞态
		// 把主动关闭记成异常退出。
		a.deliberate.Store(true)
		_ = a.stdin.Close()
		executil.KillProcessTree(a.cmd)
		// 关读端叫醒可能还阻塞在 Read 上的读循环（孙进程持管道时 EOF 不会来）。
		if a.stdout != nil {
			_ = a.stdout.Close()
		}
	})
	a.failPending(io.EOF)
	return nil
}
