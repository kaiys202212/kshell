package desktop

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
func (f fakeProvider) NewSessionCmd(ws, bin string, ctx []string) providers.Launch {
	l := f.newSess
	l.Dir = ws
	l.Args = append([]string{}, ctx...) // 透传上下文篮，供断言注入链路
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
	if len(*events) != 1 || (*events)[0] != "scan:done" {
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

func TestNewSessionLaunchesWindowWithBasket(t *testing.T) {
	app, stub, _ := newTestApp(t)
	app.runScan()
	setTools(t, app, fakeTools)
	app.mu.Lock()
	app.basket = []string{"main.go", "internal/launch/launch.go"}
	app.mu.Unlock()

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
	// 上下文篮应透传给 provider 并出现在窗口语句里
	for _, f := range []string{"main.go", "internal/launch/launch.go"} {
		if !strings.Contains(call.args[0], "'"+f+"'") {
			t.Fatalf("窗口内命令缺少上下文篮文件 %q: %v", f, call.args)
		}
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

	if len(*events) != 2 || (*events)[0] != "scan:done" || (*events)[1] != "scan:done" {
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

func TestPsStatementQuotesArgs(t *testing.T) {
	got := psStatement("claude", []string{"--resume", "it's s1"})
	want := `& 'claude' '--resume' 'it''s s1'`
	if got != want {
		t.Fatalf("psStatement = %q, 期望 %q", got, want)
	}
}
