package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/terminal"
)

// stubTermHandle 是内存终端桩：输出由测试 feed 注入，退出码由 finish 指定。
type stubTermHandle struct {
	data   chan []byte
	closed chan struct{}
	exited chan struct{}

	mu      sync.Mutex
	code    int
	pending []byte
	written []byte
	resizes [][2]int

	closeOnce sync.Once
	waitOnce  sync.Once
	finOnce   sync.Once
}

func newStubTermHandle() *stubTermHandle {
	return &stubTermHandle{
		data:   make(chan []byte, 16),
		closed: make(chan struct{}),
		exited: make(chan struct{}),
	}
}

func (h *stubTermHandle) feed(s string) { h.data <- []byte(s) }

func (h *stubTermHandle) finish(code int) {
	h.finOnce.Do(func() {
		h.mu.Lock()
		h.code = code
		h.mu.Unlock()
		close(h.exited)
	})
}

func (h *stubTermHandle) Read(b []byte) (int, error) {
	if len(h.pending) == 0 {
		select {
		case chunk := <-h.data:
			h.pending = chunk
		default:
			select {
			case chunk := <-h.data:
				h.pending = chunk
			case <-h.closed:
				return 0, io.ErrClosedPipe
			case <-h.exited:
				return 0, io.EOF
			}
		}
	}
	n := copy(b, h.pending)
	h.pending = h.pending[n:]
	return n, nil
}

func (h *stubTermHandle) Write(b []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.written = append(h.written, b...)
	return len(b), nil
}

func (h *stubTermHandle) Resize(cols, rows int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resizes = append(h.resizes, [2]int{cols, rows})
	return nil
}

func (h *stubTermHandle) Wait() (int, error) {
	h.waitOnce.Do(func() {
		select {
		case <-h.exited:
		case <-h.closed:
		}
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.code, nil
}

func (h *stubTermHandle) Close() error {
	h.closeOnce.Do(func() { close(h.closed) })
	return nil
}

func (h *stubTermHandle) writes() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.written)
}

func (h *stubTermHandle) isClosed() bool {
	select {
	case <-h.closed:
		return true
	default:
		return false
	}
}

// stubTermBackend 记录每次启动的 Spec。
type stubTermBackend struct {
	mu       sync.Mutex
	specs    []terminal.Spec
	handles  []*stubTermHandle
	startErr error
}

func (b *stubTermBackend) Start(spec terminal.Spec, cols, rows int) (terminal.Handle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.startErr != nil {
		return nil, b.startErr
	}
	h := newStubTermHandle()
	b.specs = append(b.specs, spec)
	b.handles = append(b.handles, h)
	return h, nil
}

func (b *stubTermBackend) starts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.handles)
}

func (b *stubTermBackend) lastSpec() terminal.Spec {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.specs) == 0 {
		return terminal.Spec{}
	}
	return b.specs[len(b.specs)-1]
}

func (b *stubTermBackend) lastHandle() *stubTermHandle {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.handles) == 0 {
		return nil
	}
	return b.handles[len(b.handles)-1]
}

// eventLog 收集 Emit 出去的事件。
type eventLog struct {
	mu     sync.Mutex
	events []capturedEvent
	ch     chan capturedEvent
}

type capturedEvent struct {
	name    string
	payload map[string]any
}

func newEventLog() *eventLog { return &eventLog{ch: make(chan capturedEvent, 32)} }

func (l *eventLog) emit(name string, data ...any) {
	ev := capturedEvent{name: name}
	if len(data) == 1 {
		if m, ok := data[0].(map[string]any); ok {
			ev.payload = m
		}
	}
	l.mu.Lock()
	l.events = append(l.events, ev)
	l.mu.Unlock()
	select {
	case l.ch <- ev:
	default:
	}
}

func (l *eventLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.events))
	for _, ev := range l.events {
		out = append(out, ev.name)
	}
	return out
}

// waitEvent 等一个指定名字的事件（超时失败）。
func (l *eventLog) waitEvent(t *testing.T, name string) capturedEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-l.ch:
			if ev.name == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("等待事件 %s 超时, 已收到 %v", name, l.names())
		}
	}
}

// recordingProvider 记录 NewSessionCmd/ResumeCmd 收到的可执行文件路径，
// 用来断言「工具选择」确实透传到了 launch 层。
type recordingProvider struct {
	id   string
	name string

	mu      sync.Mutex
	lastBin string
}

func (p *recordingProvider) ID() string                             { return p.id }
func (p *recordingProvider) DisplayName() string                    { return p.name }
func (p *recordingProvider) DetectSpec(string) providers.DetectSpec { return providers.DetectSpec{} }
func (p *recordingProvider) SessionRoots(string) []string           { return nil }
func (p *recordingProvider) SessionFilePattern() string             { return "*.jsonl" }
func (p *recordingProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *recordingProvider) NewSessionCmd(ws, bin string) providers.Launch {
	p.record(bin)
	return providers.Launch{Path: bin, Dir: ws}
}

func (p *recordingProvider) ResumeCmd(s providers.Session, bin string) providers.Launch {
	p.record(bin)
	return providers.Launch{Path: bin, Dir: s.Workspace, Args: []string{"--resume", s.ID}}
}

func (p *recordingProvider) record(bin string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastBin = bin
}

func (p *recordingProvider) bin() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastBin
}

// termTestEnv 是一次终端测试的全部桩件。
type termTestEnv struct {
	app     *App
	backend *stubTermBackend
	events  *eventLog
	launch  *stubLauncher
	claude  *recordingProvider
	codex   *recordingProvider
}

func newTerminalTestApp(t *testing.T) *termTestEnv {
	t.Helper()

	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "修复上传白名单", UpdatedAt: time.Now()},
	}
	res := &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}

	claude := &recordingProvider{id: "claude", name: "Claude Code"}
	codex := &recordingProvider{id: "codex", name: "Codex CLI"}
	backend := &stubTermBackend{}
	events := newEventLog()
	launcher := &stubLauncher{}

	app := NewAppWith(Options{
		Home:      "D:/home",
		CachePath: "D:/home/.kshell/cache/index.json",
		Providers: []providers.Provider{claude, codex},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return res, nil
		},
		Windows: NewWindowManager(launcher, nil),
		// 走真实装配函数（只把后端与 emit 换成桩），事件名/payload 与线上一致
		Terminals: newTerminalManagerWith(events.emit, backend),
		Emit:      events.emit,
	})

	app.runScan()
	setTools(t, app, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true},
		{ID: "codex", Name: "Codex CLI", BinPath: "codex", Installed: true},
	})
	return &termTestEnv{app: app, backend: backend, events: events, launch: launcher, claude: claude, codex: codex}
}

func TestOpenSessionTerminalOpensAndReuses(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenSessionTerminal("s1", 100, 30)
	if err != nil {
		t.Fatalf("OpenSessionTerminal error: %v", err)
	}
	if info.ID == "" {
		t.Fatal("应返回终端 ID")
	}
	if info.Kind != terminal.KindSession || info.SessionID != "s1" {
		t.Fatalf("Info 会话字段 = %+v", info)
	}
	if info.Workspace != `D:\ws-a` || info.Title != "修复上传白名单" || info.ToolID != "claude" {
		t.Fatalf("Info 未带上会话上下文 = %+v", info)
	}
	if info.Status != terminal.StatusRunning || info.Cols != 100 || info.Rows != 30 {
		t.Fatalf("Info 状态/尺寸 = %+v", info)
	}

	spec := env.backend.lastSpec()
	// 恢复命令仍在参数末尾；前面可以有会话级 MCP 注入（--mcp-config）。
	args := spec.Args
	if spec.Path != "claude" || len(args) < 2 || args[len(args)-2] != "--resume" || args[len(args)-1] != "s1" {
		t.Fatalf("Spec 未按会话恢复命令组装 = %+v", spec)
	}
	if spec.Dir != `D:\ws-a` {
		t.Fatalf("Spec.Dir = %q, 期望会话工作区", spec.Dir)
	}
	if joined := strings.Join(spec.Env, "\n"); !strings.Contains(joined, "COLORFGBG=") {
		t.Fatalf("Spec.Env 未透传主题变量 COLORFGBG: %v", spec.Env)
	}

	again, err := env.app.OpenSessionTerminal("s1", 100, 30)
	if err != nil {
		t.Fatalf("重复 OpenSessionTerminal error: %v", err)
	}
	if again.ID != info.ID || env.backend.starts() != 1 {
		t.Fatalf("同一会话应复用终端: %+v, starts = %d", again, env.backend.starts())
	}
}

func TestOpenSessionTerminalEmitsDataAndExit(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenSessionTerminal("s1", 80, 24)
	if err != nil {
		t.Fatalf("OpenSessionTerminal error: %v", err)
	}

	env.backend.lastHandle().feed("hello")
	ev := env.events.waitEvent(t, "terminal:data")
	if ev.payload["id"] != info.ID {
		t.Fatalf("terminal:data.id = %v, 期望 %q", ev.payload["id"], info.ID)
	}
	data, _ := ev.payload["data"].(string)
	got, err := base64.StdEncoding.DecodeString(data)
	if err != nil || string(got) != "hello" {
		t.Fatalf("terminal:data.data = %q, 解码 = %q, err = %v", data, got, err)
	}

	env.backend.lastHandle().finish(9)
	ev = env.events.waitEvent(t, "terminal:exit")
	if ev.payload["id"] != info.ID {
		t.Fatalf("terminal:exit.id = %v", ev.payload["id"])
	}
	if code, ok := ev.payload["exitCode"].(int); !ok || code != 9 {
		t.Fatalf("terminal:exit.exitCode = %v, 期望 9", ev.payload["exitCode"])
	}
}

func TestOpenSessionTerminalUnknownSession(t *testing.T) {
	env := newTerminalTestApp(t)

	if _, err := env.app.OpenSessionTerminal("nope", 80, 24); !errors.Is(err, errSessionNotFound) {
		t.Fatalf("未知会话应返回 errSessionNotFound, got %v", err)
	}
	if env.backend.starts() != 0 {
		t.Fatal("未知会话不应启动进程")
	}
}

func TestOpenWorkspaceTerminalSelectsTool(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenWorkspaceTerminal(`D:\ws-a`, "codex", 120, 40)
	if err != nil {
		t.Fatalf("OpenWorkspaceTerminal error: %v", err)
	}
	if info.Kind != terminal.KindNew || info.ToolID != "codex" || info.Workspace != `D:\ws-a` {
		t.Fatalf("Info = %+v", info)
	}
	if info.Title != "ws-a · Codex CLI" {
		t.Fatalf("Title = %q, 期望工作区名 + 工具展示名", info.Title)
	}
	if spec := env.backend.lastSpec(); spec.Path != "codex" || spec.Dir != `D:\ws-a` {
		t.Fatalf("Spec 未用指定工具 = %+v", spec)
	} else if joined := strings.Join(spec.Env, "\n"); !strings.Contains(joined, "COLORFGBG=") {
		t.Fatalf("Spec.Env 未透传主题变量 COLORFGBG: %v", spec.Env)
	}
	if env.codex.bin() != "codex" {
		t.Fatalf("toolID 未透传到 launch, bin = %q", env.codex.bin())
	}

	// 空 toolID：回退到工作区首选（claude 会话数最多）
	info2, err := env.app.OpenWorkspaceTerminal(`D:\ws-a`, "", 120, 40)
	if err != nil {
		t.Fatalf("OpenWorkspaceTerminal(空 toolID) error: %v", err)
	}
	if info2.ToolID != "" || info2.Title != "ws-a · Claude Code" {
		t.Fatalf("空 toolID 的 Info = %+v", info2)
	}
	if spec := env.backend.lastSpec(); spec.Path != "claude" {
		t.Fatalf("空 toolID 应用首选工具 = %+v", spec)
	}
	if info2.ID == info.ID {
		t.Fatal("每次新建终端都应是独立进程")
	}
	if env.backend.starts() != 2 {
		t.Fatalf("应起 2 个进程, got %d", env.backend.starts())
	}
}

func TestOpenShellTerminalOpensLocalShell(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenShellTerminal(`D:\ws-a`, 80, 24)
	if err != nil {
		t.Fatalf("OpenShellTerminal error: %v", err)
	}
	if info.Kind != terminal.KindShell {
		t.Fatalf("Kind = %q, want %q", info.Kind, terminal.KindShell)
	}
	if info.Workspace != `D:\ws-a` || info.Title == "" {
		t.Fatalf("Info 上下文 = %+v", info)
	}
	if info.ToolID != "" {
		t.Fatalf("本地 shell 不应带 ToolID, got %q", info.ToolID)
	}
	spec := env.backend.lastSpec()
	if spec.Dir != `D:\ws-a` {
		t.Fatalf("Spec.Dir = %q", spec.Dir)
	}
	if spec.Path == "" {
		t.Fatal("应启动本地 shell 可执行文件")
	}
	if runtime.GOOS == "windows" {
		joined := strings.Join(spec.Args, " ")
		if !strings.Contains(joined, "Set-Location") || !strings.Contains(joined, `D:\ws-a`) {
			t.Fatalf("Windows 本地 shell 应用 Set-Location 落到工作区, args=%v", spec.Args)
		}
	}

	info2, err := env.app.OpenShellTerminal(`D:\ws-a`, 80, 24)
	if err != nil {
		t.Fatalf("第二次 OpenShellTerminal error: %v", err)
	}
	if info2.ID == info.ID {
		t.Fatal("每次新建 shell 都应是独立进程")
	}
	if env.backend.starts() != 2 {
		t.Fatalf("应起 2 个进程, got %d", env.backend.starts())
	}
}

func TestOpenShellTerminalUnknownWorkspace(t *testing.T) {
	env := newTerminalTestApp(t)
	if _, err := env.app.OpenShellTerminal(`D:\nope`, 80, 24); !errors.Is(err, errWorkspaceNotFound) {
		t.Fatalf("未知工作区应返回 errWorkspaceNotFound, got %v", err)
	}
}

func TestOpenSSHTerminalOpensEmbedded(t *testing.T) {
	env := newTerminalTestApp(t)
	store := remote.NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	if _, err := store.Add(remote.Connection{
		ID: "c1", Name: "测试机", Host: "10.0.0.8", User: "root", Port: 22, Workspace: `D:\ws-a`,
	}); err != nil {
		t.Fatalf("添加连接失败: %v", err)
	}
	env.app.mu.Lock()
	env.app.opts.Store = store
	env.app.mu.Unlock()

	info, err := env.app.OpenSSHTerminal("c1", 100, 30)
	if err != nil {
		t.Fatalf("OpenSSHTerminal error: %v", err)
	}
	if info.Kind != terminal.KindSSH {
		t.Fatalf("Kind = %q, want %q", info.Kind, terminal.KindSSH)
	}
	if info.ConnID != "c1" || info.Title != "测试机" {
		t.Fatalf("SSH Info = %+v", info)
	}
	if info.Workspace != `D:\ws-a` {
		t.Fatalf("Workspace = %q, 期望连接绑定工作区", info.Workspace)
	}
	spec := env.backend.lastSpec()
	if !strings.Contains(strings.ToLower(spec.Path), "ssh") {
		t.Fatalf("应启动 ssh, Path=%q", spec.Path)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"BatchMode=yes", "-t", "root@10.0.0.8"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("SSH args 缺少 %s: %v", want, spec.Args)
		}
	}

	info2, err := env.app.OpenSSHTerminal("c1", 100, 30)
	if err != nil {
		t.Fatalf("第二次 OpenSSHTerminal error: %v", err)
	}
	if info2.ID == info.ID {
		t.Fatal("每次双击应新开独立 SSH 终端")
	}
}

func TestOpenSSHTerminalUnknownConn(t *testing.T) {
	env := newTerminalTestApp(t)
	if _, err := env.app.OpenSSHTerminal("nope", 80, 24); err == nil {
		t.Fatal("未知连接应报错")
	}
}

func TestOpenWorkspaceTerminalErrors(t *testing.T) {
	env := newTerminalTestApp(t)

	if _, err := env.app.OpenWorkspaceTerminal(`D:\nope`, "", 80, 24); !errors.Is(err, errWorkspaceNotFound) {
		t.Fatalf("未知工作区应返回 errWorkspaceNotFound, got %v", err)
	}

	setTools(t, env.app, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true},
		{ID: "codex", Name: "Codex CLI", BinPath: "", Installed: true}, // 只有配置目录
	})
	if _, err := env.app.OpenWorkspaceTerminal(`D:\ws-a`, "codex", 80, 24); !errors.Is(err, launch.ErrToolNotRunnable) {
		t.Fatalf("不可执行工具应返回 ErrToolNotRunnable, got %v", err)
	}
	if env.backend.starts() != 0 {
		t.Fatal("工具不可执行时不应启动进程")
	}
}

func TestWriteTerminalBase64(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenSessionTerminal("s1", 80, 24)
	if err != nil {
		t.Fatalf("OpenSessionTerminal error: %v", err)
	}

	if err := env.app.WriteTerminal(info.ID, base64.StdEncoding.EncodeToString([]byte("ls\r"))); err != nil {
		t.Fatalf("WriteTerminal error: %v", err)
	}
	if got := env.backend.lastHandle().writes(); got != "ls\r" {
		t.Fatalf("终端收到 %q, 期望 %q", got, "ls\r")
	}

	if err := env.app.WriteTerminal(info.ID, "!!!不是 base64!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if got := env.backend.lastHandle().writes(); got != "ls\r" {
		t.Fatalf("非法输入不应写入终端, got %q", got)
	}
}

func TestScrollbackTerminalRoundTrip(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenSessionTerminal("s1", 80, 24)
	if err != nil {
		t.Fatalf("OpenSessionTerminal error: %v", err)
	}

	raw := "\x1b[31m红\x00\xff"
	env.backend.lastHandle().feed(raw)
	env.events.waitEvent(t, "terminal:data")

	encoded, err := env.app.ScrollbackTerminal(info.ID)
	if err != nil {
		t.Fatalf("ScrollbackTerminal error: %v", err)
	}
	got, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(got) != raw {
		t.Fatalf("回放数据 = %q (err %v), 期望 %q", got, err, raw)
	}

	// 已退出的终端仍可回放
	env.backend.lastHandle().finish(0)
	env.events.waitEvent(t, "terminal:exit")
	if _, err := env.app.ScrollbackTerminal(info.ID); err != nil {
		t.Fatalf("已退出终端 ScrollbackTerminal error: %v", err)
	}
}

func TestTerminalUnknownIDErrors(t *testing.T) {
	env := newTerminalTestApp(t)

	if err := env.app.WriteTerminal("nope", base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Fatal("未知终端 WriteTerminal 应报错")
	}
	if err := env.app.ResizeTerminal("nope", 80, 24); err == nil {
		t.Fatal("未知终端 ResizeTerminal 应报错")
	}
	if _, err := env.app.ScrollbackTerminal("nope"); err == nil {
		t.Fatal("未知终端 ScrollbackTerminal 应报错")
	}
	if err := env.app.CloseTerminal("nope"); err != nil {
		t.Fatalf("未知终端 CloseTerminal 应幂等地返回 nil, got %v", err)
	}
	if got := env.app.ListTerminals(); len(got) != 0 {
		t.Fatalf("无终端时 ListTerminals 应为空, got %+v", got)
	}
}

func TestTerminalMethodsNotReady(t *testing.T) {
	app := NewApp() // 未装配：不应 panic，且给出明确错误

	if _, err := app.OpenSessionTerminal("s1", 80, 24); !errors.Is(err, errSessionNotFound) {
		t.Fatalf("未装配时无会话数据, 期望 errSessionNotFound, got %v", err)
	}
	if err := app.WriteTerminal("t1", ""); !errors.Is(err, errNotReady) {
		t.Fatalf("未装配时 WriteTerminal 应返回 errNotReady, got %v", err)
	}
	if err := app.ResizeTerminal("t1", 80, 24); !errors.Is(err, errNotReady) {
		t.Fatalf("未装配时 ResizeTerminal 应返回 errNotReady, got %v", err)
	}
	if err := app.CloseTerminal("t1"); !errors.Is(err, errNotReady) {
		t.Fatalf("未装配时 CloseTerminal 应返回 errNotReady, got %v", err)
	}
	if got := app.ListTerminals(); got == nil || len(got) != 0 {
		t.Fatalf("未装配时 ListTerminals 应为空切片, got %+v", got)
	}
	if _, err := app.ScrollbackTerminal("t1"); !errors.Is(err, errNotReady) {
		t.Fatalf("未装配时 ScrollbackTerminal 应返回 errNotReady, got %v", err)
	}
	app.Shutdown(context.Background()) // nil 终端管理器不应 panic
}

func TestNewSessionWithToolPassesToolToLaunch(t *testing.T) {
	env := newTerminalTestApp(t)

	if err := env.app.NewSessionWithTool(`D:\ws-a`, "codex"); err != nil {
		t.Fatalf("NewSessionWithTool error: %v", err)
	}
	if len(env.launch.launches) != 1 {
		t.Fatalf("应弹一次终端窗口, got %d", len(env.launch.launches))
	}
	call := env.launch.launches[0]
	if call.title != "kshell · ws-a" || call.dir != `D:\ws-a` {
		t.Fatalf("窗口 title/dir = %q/%q", call.title, call.dir)
	}
	if len(call.args) != 1 || !strings.Contains(call.args[0], "& 'codex'") {
		t.Fatalf("窗口内命令未用指定工具: %v", call.args)
	}
	if !strings.Contains(call.args[0], "$env:COLORFGBG") {
		t.Fatalf("窗口内命令未透传主题变量 $env:COLORFGBG: %v", call.args)
	}
	if env.codex.bin() != "codex" {
		t.Fatalf("toolID 未透传到 launch, bin = %q", env.codex.bin())
	}
}

func TestNewSessionDelegatesToWithTool(t *testing.T) {
	env := newTerminalTestApp(t)

	if err := env.app.NewSession(`D:\ws-a`); err != nil {
		t.Fatalf("NewSession error: %v", err)
	}
	if len(env.launch.launches) != 1 {
		t.Fatalf("应弹一次终端窗口, got %d", len(env.launch.launches))
	}
	// 空 toolID 等价于原首选逻辑（该工作区 claude 会话最多）
	if got := env.launch.launches[0].args[0]; !strings.Contains(got, "& 'claude'") {
		t.Fatalf("NewSession 应用首选工具: %q", got)
	}
}

func TestNewSessionWithToolUnknownWorkspace(t *testing.T) {
	env := newTerminalTestApp(t)

	if err := env.app.NewSessionWithTool(`D:\nope`, ""); !errors.Is(err, errWorkspaceNotFound) {
		t.Fatalf("未知工作区应返回 errWorkspaceNotFound, got %v", err)
	}
	if len(env.launch.launches) != 0 {
		t.Fatal("未知工作区不应弹窗")
	}
}

func TestShutdownClosesTerminals(t *testing.T) {
	env := newTerminalTestApp(t)

	info, err := env.app.OpenSessionTerminal("s1", 80, 24)
	if err != nil {
		t.Fatalf("OpenSessionTerminal error: %v", err)
	}

	env.app.Shutdown(context.Background())

	if !env.backend.lastHandle().isClosed() {
		t.Fatal("Shutdown 应关闭终端句柄")
	}
	if err := env.app.WriteTerminal(info.ID, base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Fatal("Shutdown 后不应还能写入终端")
	}
	list := env.app.ListTerminals()
	if len(list) != 1 || list[0].Status != terminal.StatusExited {
		t.Fatalf("Shutdown 后 ListTerminals = %+v", list)
	}
}

// TestNewTerminalManagerWithIsIdempotent 覆盖 initRealDeps 的装配约定：
// 已注入的终端管理器不得被重建（重建会丢掉既有终端会话）。
func TestNewTerminalManagerWithIsIdempotent(t *testing.T) {
	env := newTerminalTestApp(t)
	injected := env.app.opts.Terminals

	// initRealDeps 只在 Terminals 为 nil 时装配；此处直接验证装配函数产出独立实例，
	// 注入的那份不受影响（不跑 initRealDeps 是为了不触碰真实用户目录）。
	if other := newTerminalManagerWith(env.app.Emit, env.backend); other == injected {
		t.Fatal("装配函数应产出独立的终端管理器")
	}
	if env.app.opts.Terminals != injected {
		t.Fatal("注入的终端管理器不应被改动")
	}
}

// TestNewTerminalManagerForwardsEvents 直接验证真实装配函数的回调：
// 事件名固定为 terminal:data / terminal:exit，payload 为 id + base64 / id + exitCode。
func TestNewTerminalManagerForwardsEvents(t *testing.T) {
	env := newTerminalTestApp(t)
	m := newTerminalManagerWith(env.events.emit, env.backend)

	info, err := m.Open("k1", terminal.Info{Kind: terminal.KindNew, Title: "t"}, terminal.Spec{Path: "claude"}, 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	env.backend.lastHandle().feed("hi")
	ev := env.events.waitEvent(t, "terminal:data")
	if ev.payload["id"] != info.ID {
		t.Fatalf("terminal:data.id = %v", ev.payload["id"])
	}
	if got, _ := base64.StdEncoding.DecodeString(ev.payload["data"].(string)); string(got) != "hi" {
		t.Fatalf("terminal:data.data 解码 = %q", got)
	}
	env.backend.lastHandle().finish(0)
	ev = env.events.waitEvent(t, "terminal:exit")
	if code, _ := ev.payload["exitCode"].(int); code != 0 {
		t.Fatalf("terminal:exit.exitCode = %v", ev.payload["exitCode"])
	}
	m.CloseAll()
}
