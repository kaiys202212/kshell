package desktop

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/workspace"
)

var (
	errNotReady          = errors.New("桌面版尚未初始化完成")
	errSessionNotFound   = errors.New("会话不存在或已被清理")
	errWorkspaceNotFound = errors.New("工作区不存在")
)

// scanFunc 是 discovery.Scan 的签名抽象，注入便于测试。
type scanFunc func(home string, ps []providers.Provider, cachePath string, opts discovery.ScanOptions) (*discovery.Result, error)

// Options 桌面版装配选项；零值字段在 Startup 里用真实环境补齐（测试可注入）。
type Options struct {
	Home          string
	CachePath     string
	ProvidersPath string // providers.yaml 完整路径；设置页读写用
	Config        config.Config
	Providers     []providers.Provider
	Store         *remote.Store // SSH 连接存储；nil 时 Startup 按真实环境装配
	Scan          scanFunc
	Windows       *WindowManager
	Emit          func(name string, data ...any)
	TrayIcon      []byte // 托盘图标数据（Windows 用 ICO，其它平台用 PNG）；nil 表示不启用托盘
}

// App 是暴露给前端的绑定对象：薄封装 discovery/providers/window 等核心包，
// 只做参数组装、状态缓存与事件推送，业务逻辑在各自包里。
// 绑定方法会被 Wails 在多个 goroutine 调用，共享状态由 mu 保护；
// opts 在 Startup 装配完成后视为只读，读取一律经 snapshot 加锁拷贝。
type App struct {
	mu   sync.Mutex
	ctx  context.Context
	opts Options

	tools    []discovery.Tool
	result   *discovery.Result
	scanning bool
	basket   []string // 上下文篮：文件视图勾选的文件，新建会话时注入初始提示
	quitting bool     // 托盘「退出」已发起：BeforeClose 据此放行 Wails 的退出流程

	trees   map[string]*workspace.Tree // 工作区路径 → 文件树（懒创建，节点级缓存）
	treeMu  sync.Mutex                 // 树操作串行化（Expand 会写节点，不能只靠 mu 快照）
}

// NewApp 创建绑定对象；真实依赖延迟到 Startup 装配（包级初始化时还拿不到用户目录）。
func NewApp() *App { return &App{} }

// NewAppWith 用给定选项创建绑定对象（测试注入用）。
func NewAppWith(o Options) *App { return &App{opts: o} }

// Startup 由 Wails 在窗口启动后调用：装配真实依赖、启动托盘并触发后台扫描。
func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	if a.opts.Emit == nil && ctx != nil {
		a.opts.Emit = func(name string, data ...any) { runtime.EventsEmit(ctx, name, data...) }
	}
	a.mu.Unlock()

	a.initRealDeps()
	a.StartTray()
	go a.runScan()
	go a.reapLoop()
}

// initRealDeps 用真实环境补齐未注入的选项（与 cmd/kshell 的 TUI 装配保持同源逻辑）。
// 不做整体早退：逐字段补齐，保证「部分注入」场景（如注入了 Home 但没注入 Store）
// 也能拿到完整依赖，错误是显式的而非静默零值。
func (a *App) initRealDeps() {
	home, err := os.UserHomeDir()
	if err != nil {
		return // 无主目录时保持未就绪状态，ScanSessions 会返回 errNotReady
	}
	paths, err := config.Paths()
	if err != nil {
		return
	}
	_ = config.EnsureRoot(paths)

	cfg, _ := config.Load(paths) // 读不到配置就用默认值，不阻断启动

	_ = providers.EnsureProvidersFile(paths.Providers)
	ps := []providers.Provider{providers.Claude{}, providers.Codex{}, providers.Gemini{}}
	if specs, err := providers.LoadGenericSpecs(paths.Providers); err == nil {
		for _, spec := range specs {
			ps = append(ps, providers.Generic{Spec: spec, Home: home})
		}
	}

	var wm *WindowManager
	if defaultLauncherFactory != nil {
		wm = NewWindowManager(defaultLauncherFactory(), func(title string) {
			a.Emit("window:closed", title)
		})
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.opts.Home == "" {
		a.opts.Home = home
	}
	if a.opts.CachePath == "" {
		a.opts.CachePath = paths.CacheIndex
	}
	if a.opts.ProvidersPath == "" {
		a.opts.ProvidersPath = paths.Providers
	}
	if a.opts.Config.MaxDepth <= 0 {
		a.opts.Config = cfg
	}
	if len(a.opts.Providers) == 0 {
		a.opts.Providers = ps
	}
	if a.opts.Scan == nil {
		a.opts.Scan = discovery.Scan
	}
	if a.opts.Windows == nil {
		a.opts.Windows = wm
	}
	if a.opts.Store == nil {
		store := remote.NewStore(paths.Connections)
		_ = store.Load() // 读不到连接文件不阻断启动，与 TUI 行为一致
		a.opts.Store = store
	}
}

// ready 报告选项是否已装配完整（全部走注入或已补齐真实依赖）。
func (o Options) ready() bool {
	return o.Home != "" && o.Scan != nil && o.Windows != nil
}

// reapInterval 是窗口回收轮询间隔。取值需小于宽限期（3s，见 WindowManager.reapGrace）：
// 轮询本身不做判定，只保证宽限期一过、最多再等一个间隔就能把 window:closed 推给前端，
// 前端行状态及时还原；再调小只会增加无效探测（Reap 有宽限期门槛，代价极低）。
const reapInterval = 2 * time.Second

// reapOnce 执行一次窗口回收：Reap 发现死亡窗口后经 onClosed 回调（锁外执行）推送
// window:closed 事件。单次回收与定时循环（reapLoop）分开：前者可测，后者只是 ticker 包装。
func (a *App) reapOnce() {
	wm := a.snapshot().Windows
	if wm == nil {
		return
	}
	wm.Reap()
}

// reapLoop 定期回收死亡终端窗口。与窗口同生命周期，无需取消机制：
// 进程随 Wails 主循环退出，goroutine 一并结束。
func (a *App) reapLoop() {
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.reapOnce()
	}
}

// StartTray 启动系统托盘（未注入托盘图标或 Wails 上下文未就绪时跳过）。
// systray.Run 独占调用方 goroutine 跑消息循环，必须在 goroutine 里启动。
func (a *App) StartTray() {
	a.mu.Lock()
	icon := a.opts.TrayIcon
	ctx := a.ctx
	a.mu.Unlock()
	if len(icon) == 0 || ctx == nil {
		return
	}
	go runTray(icon,
		func() { runtime.WindowShow(ctx) },
		func() { a.quitApp(ctx) },
	)
}

// quitApp 托盘「退出」入口：置退出标记并请求 Wails 退出进程。
// 注意 runtime.Quit 内部会再走一次 BeforeClose（见 wails windows frontend.Quit），
// quitting 标记保证那次调用放行，否则「退出」会被拦截成隐藏窗口、进程永远退不出去。
func (a *App) quitApp(ctx context.Context) {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	quitTrayLoop() // 先停托盘消息循环（通知区图标随之移除），再退进程
	runtime.Quit(ctx)
}

// BeforeClose 供 Wails OnBeforeClose 挂载：拦截窗口关闭改为隐藏到托盘（返回 true）；
// 托盘「退出」期间放行（返回 false），让 runtime.Quit 真正退出进程。
func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	a.mu.Unlock()
	if quitting {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

// snapshot 加锁拷贝一份选项，调用方在锁外使用副本。
func (a *App) snapshot() Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts
}

// ScanSessions 触发一次会话扫描（已在扫则不重复），立即返回最近一次结果；
// 前端首次调用拿到 nil 属正常，等 "scan:done" 事件后再刷新。
// 未就绪（装配未完成）时返回 errNotReady 且不触发扫描。
func (a *App) ScanSessions() (*discovery.Result, error) {
	a.mu.Lock()
	res := a.result
	started := false
	if !a.scanning && a.opts.ready() {
		a.scanning = true
		started = true
	}
	a.mu.Unlock()

	if started {
		go a.runScan()
	}
	if !a.snapshot().ready() {
		return res, errNotReady
	}
	return res, nil
}

// runScan 执行一次扫描并推送 "scan:done" 事件（无论成败，前端据此刷新）。
// 失败时保留上一次成功结果，避免一次瞬时失败让已知会话/工作区全面失联。
func (a *App) runScan() {
	o := a.snapshot()
	if !o.ready() {
		a.Emit("scan:done", map[string]any{"failed": 0, "error": errNotReady.Error()})
		return
	}
	tools := discovery.DetectAll(o.Home, o.Providers)
	res, err := o.Scan(o.Home, o.Providers, o.CachePath, discovery.ScanOptions{
		Roots:    o.Config.ScanRoots,
		MaxDepth: o.Config.MaxDepth,
		Exclude:  o.Config.Exclude,
	})

	a.mu.Lock()
	a.tools = tools
	if err == nil {
		a.result = res
	}
	a.scanning = false
	a.mu.Unlock()

	ev := map[string]any{"failed": 0}
	if err != nil {
		ev["error"] = err.Error()
	}
	if res != nil {
		ev["sessions"] = res.Sessions
		ev["workspaces"] = res.Workspaces
		ev["failed"] = len(res.Failed)
	}
	a.Emit("scan:done", ev)
}

// GetWorkspaces 返回最近一次扫描的工作区列表（未扫完时为空切片）。
// 返回值是共享切片：Result 整体替换、替换后只读，调用方不得原地修改。
func (a *App) GetWorkspaces() []discovery.Workspace {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result == nil {
		return []discovery.Workspace{}
	}
	return a.result.Workspaces
}

// GetSessions 返回最近一次扫描的会话列表（未扫完时为空切片）。
// 与 ScanSessions 不同，本方法只读缓存、不触发新的后台扫描，
// 供前端会话列表反复刷新用，避免和 scan:done 事件形成循环。
// 返回值是共享切片：Result 整体替换、替换后只读，调用方不得原地修改。
func (a *App) GetSessions() []providers.Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result == nil {
		return []providers.Session{}
	}
	return a.result.Sessions
}

// ResumeSession 恢复指定会话：弹出新终端窗口并在其中启动 agent CLI。
// 同一标题的窗口已存在时复用聚焦，不会重复弹窗。
func (a *App) ResumeSession(id string) error {
	s, tools, ok := a.sessionByID(id)
	if !ok {
		return errSessionNotFound
	}
	o := a.snapshot()
	l, err := launch.ForSession(o.Providers, tools, s)
	if err != nil {
		return err
	}
	return a.launchWindow(o.Windows, l, s.Title)
}

// NewSession 在指定工作区新建会话（wsID 为工作区路径），上下文篮文件作为初始提示注入。
func (a *App) NewSession(wsID string) error {
	ws, tools, ok := a.workspaceByID(wsID)
	if !ok {
		return errWorkspaceNotFound
	}
	o := a.snapshot()
	l, err := launch.ForWorkspace(o.Providers, tools, ws, a.basketSnapshot())
	if err != nil {
		return err
	}
	return a.launchWindow(o.Windows, l, ws.Name)
}

// FocusSession 聚焦指定会话已打开的终端窗口；未打开过返回 false。
func (a *App) FocusSession(id string) bool {
	s, _, ok := a.sessionByID(id)
	if !ok {
		return false
	}
	o := a.snapshot()
	if o.Windows == nil {
		return false
	}
	return o.Windows.Focus(o.Windows.TerminalTitle(s.Title))
}

// launchWindow 把启动描述转成窗口内的 PowerShell 语句并弹窗。
// 注意不走 launcher.Build 的 shim 解析：弹出的窗口本身就是 PowerShell，
// .ps1 CLI 用 & 调用运算符即可原生执行，无需转 .cmd。
func (a *App) launchWindow(wm *WindowManager, l providers.Launch, titleText string) error {
	if wm == nil {
		return errNotReady
	}
	return wm.LaunchSession(l.Dir, titleText, psStatement(l.Path, l.Args))
}

// psStatement 生成在已打开的 PowerShell 窗口里执行 CLI 的语句：
// & 'bin' 'arg1' 'arg2'（调用运算符 + 单引号字面量，内嵌单引号双写转义）。
func psStatement(bin string, args []string) string {
	s := "& " + psQuote(bin)
	for _, arg := range args {
		s += " " + psQuote(arg)
	}
	return s
}

func (a *App) sessionByID(id string) (providers.Session, []discovery.Tool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result != nil {
		for _, s := range a.result.Sessions {
			if s.ID == id {
				return s, a.tools, true
			}
		}
	}
	return providers.Session{}, nil, false
}

func (a *App) workspaceByID(id string) (discovery.Workspace, []discovery.Tool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result != nil {
		for _, ws := range a.result.Workspaces {
			if ws.Path == id {
				return ws, a.tools, true
			}
		}
	}
	return discovery.Workspace{}, nil, false
}

// basketSnapshot 返回上下文篮的副本，避免扫描/启动并发读写同一切片。
func (a *App) basketSnapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.basket))
	copy(out, a.basket)
	return out
}

// Emit 对外转发事件（窗口关闭回调等内部使用；未就绪时静默丢弃）。
func (a *App) Emit(name string, data ...any) {
	emit := a.snapshot().Emit
	if emit != nil {
		emit(name, data...)
	}
}
