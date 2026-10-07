package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
)

// fakeProvider 固定返回启动描述，便于断言桌面层的组装结果。
type fakeProvider struct {
	id      string
	resume  providers.Launch
	newSess providers.Launch
}

func (f fakeProvider) ID() string                             { return f.id }
func (f fakeProvider) DisplayName() string                    { return f.id }
func (f fakeProvider) DetectSpec(string) providers.DetectSpec { return providers.DetectSpec{} }
func (f fakeProvider) SessionRoots(string) []string           { return nil }
func (f fakeProvider) SessionFilePattern() string             { return "*.jsonl" }
func (f fakeProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, errors.New("not implemented")
}
func (f fakeProvider) NewSessionCmd(ws, bin string) providers.Launch {
	l := f.newSess
	l.Dir = ws
	if bin != "" {
		l.Path = bin
	}
	return l
}
func (f fakeProvider) ResumeCmd(s providers.Session, bin string) providers.Launch {
	l := f.resume
	l.Dir = s.Workspace
	return l
}

var fakeTools = []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}

// setTools 显式落位工具表：fakeProvider 的 DetectSpec 为空，DetectAll 探测不到，
// 与真实扫描（tools 来自 DetectAll）不同，这里手动对齐可执行状态。
func setTools(t *testing.T, app *App, tools []discovery.Tool) {
	t.Helper()
	app.mu.Lock()
	app.tools = tools
	app.toolsReady = true
	app.mu.Unlock()
}

// newTestApp 组装一个扫描结果固定、弹窗打桩的 App。
func newTestApp(t *testing.T) (*App, *stubLauncher, *[]string) {
	t.Helper()

	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "修复上传白名单", UpdatedAt: time.Now()},
	}
	workspaces := discovery.GroupSessions(sessions)
	res := &discovery.Result{Sessions: sessions, Workspaces: workspaces}

	stub := &stubLauncher{}
	var events []string
	app := NewAppWith(Options{
		Home:      "D:/home",
		CachePath: "D:/home/.kshell/cache/index.json",
		Providers: []providers.Provider{fakeProvider{
			id:      "claude",
			resume:  providers.Launch{Path: "claude", Args: []string{"--resume", "s1"}},
			newSess: providers.Launch{Path: "claude"},
		}},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return res, nil
		},
		Windows: NewWindowManager(stub, nil),
		Emit:    func(name string, _ ...any) { events = append(events, name) },
	})
	return app, stub, &events
}

func TestScanSessionsDelegatesToDiscovery(t *testing.T) {
	app, _, events := newTestApp(t)

	// runScan 直接同步执行，验证委托与状态落位（异步触发由下一个测试覆盖）
	app.runScan()

	if len(app.result.Sessions) != 1 || len(app.result.Workspaces) != 1 {
		t.Fatalf("扫描后 result 未落位: %+v", app.result)
	}
	if len(*events) < 1 || (*events)[len(*events)-1] != "scan:done" {
		t.Fatalf("扫描完成应推送 scan:done, got %v", *events)
	}
	if got := app.GetWorkspaces(); len(got) != 1 || got[0].Path != `D:\ws-a` {
		t.Fatalf("GetWorkspaces = %+v", got)
	}
}

func TestGetSessions(t *testing.T) {
	app, _, _ := newTestApp(t)

	if got := app.GetSessions(); got == nil || len(got) != 0 {
		t.Fatalf("未扫描时 GetSessions 应为空切片, got %+v", got)
	}
	app.runScan()
	got := app.GetSessions()
	if len(got) != 1 || got[0].ID != "s1" {
		t.Fatalf("GetSessions = %+v", got)
	}

	// 未装配的 App 也不应返回 nil，避免前端拿到 null
	if got := (NewApp()).GetSessions(); got == nil || len(got) != 0 {
		t.Fatalf("未装配时 GetSessions 应为空切片, got %+v", got)
	}
}

func TestGetSessionPreview(t *testing.T) {
	applang.SetForTest(t, "en")
	app, _, _ := newTestApp(t)
	app.runScan()
	app.mu.Lock()
	app.result.Sessions[0].Path = filepath.Join("..", "providers", "testdata", "claude", "basic.jsonl")
	app.mu.Unlock()

	got, err := app.GetSessionPreview("s1")
	if err != nil {
		t.Fatalf("GetSessionPreview: %v", err)
	}
	if !strings.Contains(got.Markdown, "## User") || !strings.Contains(got.Markdown, "修复上传白名单校验") {
		t.Fatalf("Markdown = %q", got.Markdown)
	}

	if _, err := app.GetSessionPreview("nope"); !errors.Is(err, errSessionNotFound) {
		t.Fatalf("未知会话应返回 errSessionNotFound, got %v", err)
	}
}

// TestWorkspaceByIDNormalizesPath 验证页签 id 与扫描 Path 形态不同（盘符大小写、
// 分隔符）时仍能匹配：持久化页签在两次扫描之间形态漂移（Workspace.Path 取自
// 第一个出现的会话 cwd），不再误报「工作区不存在」。
func TestWorkspaceByIDNormalizesPath(t *testing.T) {
	app, _, _ := newTestApp(t)
	app.runScan()

	ws, _, ok := app.workspaceByID(`d:/ws-a`)
	if !ok {
		t.Fatal("归一化后应能匹配盘符大小写/分隔符不同的同一工作区")
	}
	if ws.Path != `D:\ws-a` {
		t.Fatalf("返回的应是扫描结果原形态, got %q", ws.Path)
	}
}

// TestOpenWorkspaceTerminalWaitsForFirstScan 验证「应用刚启动、首轮扫描未完成」时
// 新建会话的查找阶段会等待扫描结果落位再判定，而不是立即误报「工作区不存在」。
// 只测查找兜底：launch 阶段的工具表由 runScan 重新探测刷新（真实环境能探到），
// 测试桩探测不到，全链路留给 TestOpenWorkspaceTerminal* 覆盖。
func TestOpenWorkspaceTerminalWaitsForFirstScan(t *testing.T) {
	env := newTerminalTestApp(t)

	// 模拟刚启动：结果未就绪（后台扫描尚未落位）
	env.app.mu.Lock()
	env.app.result = nil
	env.app.mu.Unlock()

	ws, _, ok := env.app.workspaceByIDReady(`D:\ws-a`)
	if !ok {
		t.Fatal("首扫未就绪时应等待扫描落位并重查成功")
	}
	if ws.Path != `D:\ws-a` {
		t.Fatalf("ws.Path = %q", ws.Path)
	}
}

func TestScanSessionsTriggersBackgroundScan(t *testing.T) {
	app, _, _ := newTestApp(t)

	res, err := app.ScanSessions()
	if err != nil {
		t.Fatalf("ScanSessions error: %v", err)
	}
	if res != nil {
		t.Fatalf("首次扫描应返回 nil 缓存, got %+v", res)
	}

	// 等待后台扫描完成（测试打桩的扫描立即返回，轮询足够）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		done := app.result != nil && !app.scanning
		app.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.result == nil {
		t.Fatal("后台扫描未在期限内完成")
	}
	if app.scanning {
		t.Fatal("扫描完成后 scanning 标志应复位")
	}
}

func TestScanSessionsWhileScanningDoesNotStack(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }
	defer release()

	app, _, _ := newTestApp(t)
	app.mu.Lock()
	app.opts.Scan = func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
		<-block // 挡住扫描，制造「正在扫」窗口
		return &discovery.Result{}, nil
	}
	app.mu.Unlock()

	if _, err := app.ScanSessions(); err != nil {
		t.Fatalf("ScanSessions error: %v", err)
	}
	if _, err := app.ScanSessions(); err != nil {
		t.Fatalf("ScanSessions error: %v", err)
	}
	release() // 解除扫描阻塞

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		busy := app.scanning
		app.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.scanning {
		t.Fatal("扫描未结束")
	}
}

// 扫描进行中再次 ScanSessions 不得静默丢弃：当前轮结束后应自动再扫一轮。
// 否则「新建会话后的延迟重扫」若撞上首页首扫，会话永远进不了列表，直到用户再点一次新建。
func TestScanSessionsWhileScanningQueuesFollowUp(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }
	defer release()

	var calls atomic.Int32
	app, _, events := newTestApp(t)
	app.mu.Lock()
	app.opts.Scan = func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
		n := calls.Add(1)
		if n == 1 {
			<-block // 第一轮挡住，给第二次 ScanSessions 留窗口
		}
		return &discovery.Result{Sessions: []providers.Session{
			{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "t", UpdatedAt: time.Now()},
		}}, nil
	}
	app.mu.Unlock()

	if _, err := app.ScanSessions(); err != nil {
		t.Fatalf("ScanSessions #1 error: %v", err)
	}
	// 等第一轮真正进入 Scan（挡住后 calls==1），再请求第二次
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("第一轮扫描未启动")
	}
	if _, err := app.ScanSessions(); err != nil {
		t.Fatalf("ScanSessions #2 error: %v", err)
	}
	release()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("忙碌时的 ScanSessions 应排队复扫, Scan 调用次数=%d", got)
	}
	// 两轮都要推 scan:done，前端才能拿到最终列表
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		busy := app.scanning
		app.mu.Unlock()
		if !busy && len(*events) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(*events) < 2 {
		t.Fatalf("两轮扫描都应推送 scan:done, got %v", *events)
	}
}

func TestResumeSessionLaunchesWindow(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)

	if err := app.ResumeSession("s1"); err != nil {
		t.Fatalf("ResumeSession error: %v", err)
	}

	if len(stub.launches) != 1 {
		t.Fatalf("launcher.Launch 次数 = %d, 期望 1", len(stub.launches))
	}
	call := stub.launches[0]
	if call.dir != `D:\ws-a` {
		t.Fatalf("窗口 Dir = %q, 期望会话工作区", call.dir)
	}
	if call.title != "kshell · 修复上传白名单" {
		t.Fatalf("窗口标题 = %q", call.title)
	}
	if len(call.args) != 1 || !strings.Contains(call.args[0], "& 'claude' '--resume' 's1'") {
		t.Fatalf("窗口内命令 = %v, 期望包含 & 'claude' '--resume' 's1'", call.args)
	}
	if !strings.Contains(call.args[0], "$env:COLORFGBG") {
		t.Fatalf("窗口内命令未透传主题变量 $env:COLORFGBG: %v", call.args)
	}
	// 弹窗后应登记存活表，重复恢复转聚焦
	if !app.opts.Windows.Alive(call.title) {
		t.Fatal("弹窗后应登记存活表")
	}
	if err := app.ResumeSession("s1"); err != nil {
		t.Fatalf("重复 ResumeSession error: %v", err)
	}
	if len(stub.launches) != 1 {
		t.Fatalf("重复恢复不应再弹窗, Launch 次数 = %d", len(stub.launches))
	}
	if len(stub.focuses) != 1 {
		t.Fatalf("重复恢复应转聚焦, Focus 次数 = %d", len(stub.focuses))
	}
}

func TestResumeSessionUnknownID(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)

	if err := app.ResumeSession("nope"); !errors.Is(err, errSessionNotFound) {
		t.Fatalf("未知会话应返回 errSessionNotFound, got %v", err)
	}
	if len(stub.launches) != 0 {
		t.Fatal("未知会话不应弹窗")
	}
}

func TestResumeSessionToolNotRunnable(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	app.mu.Lock()
	app.tools = []discovery.Tool{{ID: "claude", Installed: true, BinPath: ""}} // 只有配置目录
	app.mu.Unlock()

	if err := app.ResumeSession("s1"); !errors.Is(err, launch.ErrToolNotRunnable) {
		t.Fatalf("不可执行工具应返回 ErrToolNotRunnable, got %v", err)
	}
	if len(stub.launches) != 0 {
		t.Fatal("不可执行工具不应弹窗")
	}
}

func TestNewSessionLaunchesWindowAtWorkspace(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)

	if err := app.NewSession(`D:\ws-a`); err != nil {
		t.Fatalf("NewSession error: %v", err)
	}

	call := stub.launches[0]
	if call.dir != `D:\ws-a` {
		t.Fatalf("窗口 Dir = %q, 期望工作区路径", call.dir)
	}
	if call.title != "kshell · ws-a" {
		t.Fatalf("窗口标题 = %q, 期望工作区名", call.title)
	}
	// args 只含工具本身的启动命令：kshell 不再向新会话注入任何文件参数
	if len(call.args) != 1 {
		t.Fatalf("新建会话不应带额外参数: %v", call.args)
	}
}

func TestNewSessionUnknownWorkspace(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)

	if err := app.NewSession(`D:\nope`); !errors.Is(err, errWorkspaceNotFound) {
		t.Fatalf("未知工作区应返回 errWorkspaceNotFound, got %v", err)
	}
	if len(stub.launches) != 0 {
		t.Fatal("未知工作区不应弹窗")
	}
}

func TestFocusSession(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)
	stub.focusRet = true

	if app.FocusSession("s1") {
		t.Fatal("未打开过的会话 FocusSession 应返回 false")
	}
	if err := app.ResumeSession("s1"); err != nil {
		t.Fatalf("ResumeSession error: %v", err)
	}
	if !app.FocusSession("s1") {
		t.Fatal("已打开的会话 FocusSession 应返回 true")
	}
	if len(stub.focuses) != 1 || stub.focuses[0] != "kshell · 修复上传白名单" {
		t.Fatalf("launcher.Focus 收到 %v", stub.focuses)
	}
}

func TestScanFailureStillEmits(t *testing.T) {
	app, _, events := newTestApp(t)

	// 先成功扫描一次落位缓存
	app.runScan()

	// 再让扫描失败：事件照发，但上一次结果必须保留（会话/工作区不失联）
	app.mu.Lock()
	app.opts.Scan = func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
		return nil, errors.New("扫描炸了")
	}
	app.mu.Unlock()
	app.runScan()

	if countNamed(*events, "scan:done") != 2 {
		t.Fatalf("每次扫描结束都要推送 scan:done 让前端复位, got %v", *events)
	}
	if app.result == nil || len(app.result.Sessions) != 1 {
		t.Fatalf("扫描失败应保留上一次结果, got %+v", app.result)
	}
	if got := app.GetWorkspaces(); len(got) != 1 {
		t.Fatalf("扫描失败后 GetWorkspaces 不应为空, got %+v", got)
	}
}

func TestRunScanNotReadyEmitsError(t *testing.T) {
	app := NewApp()
	var payloads []map[string]any
	app.mu.Lock()
	app.opts.Emit = func(_ string, data ...any) {
		if len(data) == 1 {
			if m, ok := data[0].(map[string]any); ok {
				payloads = append(payloads, m)
			}
		}
	}
	app.mu.Unlock()

	app.runScan()

	if len(payloads) != 1 {
		t.Fatalf("未就绪 runScan 应发一次事件, got %v", payloads)
	}
	if _, ok := payloads[0]["error"]; !ok {
		t.Fatalf("未就绪 runScan 的事件应带 error, got %v", payloads[0])
	}
}

func TestScanNotReadyReturnsError(t *testing.T) {
	app := NewApp()

	res, err := app.ScanSessions()
	if !errors.Is(err, errNotReady) {
		t.Fatalf("未装配时应返回 errNotReady, got %v", err)
	}
	if res != nil {
		t.Fatalf("未装配时 result 应为 nil, got %+v", res)
	}
}

// TestReapOnceEmitsWindowClosed 验证 reapOnce（单次回收）发现死亡窗口后，
// 经 WindowManager 的 onClosed 回调把 window:closed 事件推给前端，
// 让前端把对应行的 open 状态还原。
func TestReapOnceEmitsWindowClosed(t *testing.T) {
	app, stub, events := newTestApp(t)
	// 换上带 onClosed→Emit 的管理器：走 initRealDeps 同一构造工厂，覆盖真实装配路径
	wm := newWindowManagerWithEmit(app, stub)
	wm.reapGrace = 0 // 关闭宽限期，登记后立即参与判定
	wm.newChecker = func(string) procChecker { return func(string) bool { return false } }
	app.mu.Lock()
	app.opts.Windows = wm
	app.mu.Unlock()

	app.runScan()
	setTools(t, app, fakeTools)
	if err := app.ResumeSession("s1"); err != nil {
		t.Fatalf("ResumeSession error: %v", err)
	}
	if lastNamed(*events) != "scan:done" {
		t.Fatalf("前置：Resume 前最后事件应是 scan:done, got %v", *events)
	}

	app.reapOnce()

	if lastNamed(*events) != "window:closed" {
		t.Fatalf("Reap 发现死亡窗口后应推送 window:closed, got %v", *events)
	}
	if wm.Alive("kshell · 修复上传白名单") {
		t.Fatal("死亡窗口应被 Reap 回收")
	}
}

// TestReapOnceNilWindowsNoPanic 未装配窗口管理器时（如纯测试环境）回收应静默跳过。
func TestReapOnceNilWindowsNoPanic(t *testing.T) {
	app := NewApp()
	app.reapOnce() // 不应 panic
}

func lastNamed(events []string) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1]
}

func countNamed(events []string, name string) int {
	n := 0
	for _, e := range events {
		if e == name {
			n++
		}
	}
	return n
}

func TestPsStatementQuotesArgs(t *testing.T) {
	got := psStatement("claude", []string{"--resume", "it's s1"}, nil)
	want := `& 'claude' '--resume' 'it''s s1'`
	if got != want {
		t.Fatalf("psStatement = %q, 期望 %q", got, want)
	}
}

func TestPsStatementInjectsEnv(t *testing.T) {
	got := psStatement("claude", nil, map[string]string{"COLORFGBG": "15;0"})
	want := `$env:COLORFGBG = '15;0'; & 'claude'`
	if got != want {
		t.Fatalf("psStatement = %q, want %q", got, want)
	}
}

type probeProvider struct{ binName string }

func (p probeProvider) ID() string          { return "probe" }
func (p probeProvider) DisplayName() string { return "Probe" }
func (p probeProvider) DetectSpec(string) providers.DetectSpec {
	return providers.DetectSpec{BinName: p.binName}
}
func (probeProvider) SessionRoots(string) []string { return nil }
func (probeProvider) SessionFilePattern() string   { return "*.jsonl" }
func (probeProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, errors.New("not implemented")
}
func (probeProvider) NewSessionCmd(string, string) providers.Launch { return providers.Launch{} }
func (probeProvider) ResumeCmd(providers.Session, string) providers.Launch {
	return providers.Launch{}
}

func TestGetToolsPublishedBeforeScanFinishes(t *testing.T) {
	dir := t.TempDir()
	name := "kshell-probe-cli"
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, name+".cmd"), []byte("@echo off\r\necho probe 1\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho probe 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	started := make(chan struct{})
	block := make(chan struct{})
	var startOnce sync.Once
	var mu sync.Mutex
	var events []string
	app := NewAppWith(Options{
		Home:      t.TempDir(),
		CachePath: filepath.Join(t.TempDir(), "index.json"),
		Providers: []providers.Provider{probeProvider{binName: name}},
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			startOnce.Do(func() { close(started) })
			<-block
			return &discovery.Result{}, nil
		},
		Windows: NewWindowManager(&stubLauncher{}, nil),
		Emit: func(name string, _ ...any) {
			mu.Lock()
			events = append(events, name)
			mu.Unlock()
		},
	})

	go app.runScan()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Scan 未开始")
	}

	got := app.GetTools()
	found := false
	for _, tool := range got {
		if tool.ID == "probe" && tool.Installed && tool.BinPath != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("会话扫描未结束时就应能读到已探测的 CLI, got %+v", got)
	}
	mu.Lock()
	sawTools := false
	for _, e := range events {
		if e == "tools:updated" {
			sawTools = true
			break
		}
	}
	mu.Unlock()
	if !sawTools {
		t.Fatalf("DetectAll 结束后应推 tools:updated, got %v", events)
	}
	close(block)
}
