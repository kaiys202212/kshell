package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/executil"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/terminal"
	"github.com/yangk/kshell/internal/workspace"
)

var (
	errNotReady          = errors.New("桌面版尚未初始化完成")
	errSessionNotFound   = errors.New("会话不存在或已被清理")
	errWorkspaceNotFound = errors.New("工作区不存在")
)

// configWriteMu 串行化「读内存配置 → 变更 → 落盘 → 提交内存」全过程：
// 多个 setter（关闭行为 / 颜色模式）并发调用时不会互相覆盖对方的字段。
var configWriteMu sync.Mutex

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
	Terminals     *terminal.Manager // 内嵌终端管理器；nil 时 Startup 装配真实后端（测试可注入桩）
	Chats         *chat.Manager     // ACP 聊天管理器；nil 时 Startup 装配真实后端（测试可注入桩）
	Emit          func(name string, data ...any)
	TrayIcon      []byte // 托盘图标数据（Windows 用 ICO，其它平台用 PNG）；nil 表示不启用托盘
	SignalPath    string // 退出信号文件路径；nil/空时 Startup 补齐真实路径（测试可不启用）
	// SnapshotPath 是扫描结果快照文件：启动时先端出上次结果让界面秒开，后台扫描再覆盖。
	// 空字符串表示禁用快照（测试默认如此，避免互相污染）。
	SnapshotPath string
	// ToolsCachePath 是工具版本探测缓存文件：CLI 未升级时跳过 `<bin> --version` 子进程。
	ToolsCachePath string
	// Projects 是项目表（手动添加 / 逻辑删除）；nil 时 Startup 按真实路径装配。
	Projects *discovery.ProjectStore
	// ProjectsPath 是 projects.yaml 完整路径；仅在未注入 Projects 时用于装配。
	ProjectsPath string
	// Layout 是 ~/.kshell 的路径布局，写回配置（颜色模式 / 关闭行为）时使用。
	Layout config.Layout
	// ThemeCacheDir 是主题注入文件目录，供启动 agent 时使用。
	ThemeCacheDir string
}

// App 是暴露给前端的绑定对象：薄封装 discovery/providers/window 等核心包，
// 只做参数组装、状态缓存与事件推送，业务逻辑在各自包里。
// 绑定方法会被 Wails 在多个 goroutine 调用，共享状态由 mu 保护；
// opts 在 Startup 装配完成后视为只读，读取一律经 snapshot 加锁拷贝。
type App struct {
	mu   sync.Mutex
	ctx  context.Context
	opts Options

	tools []discovery.Tool
	// rawWorkspaces 是**未叠加项目表**的扫描结果：项目列表永远从它派生，
	// 否则「删除后再还原」会因为列表已被过滤掉而回不来。
	rawWorkspaces []discovery.Workspace
	result        *discovery.Result
	scanning      bool
	// pendingScan：扫描进行中又收到 ScanSessions 时置位，本轮结束后自动再扫一轮。
	// 否则「新建会话后的延迟重扫」撞上首页首扫会被静默丢掉，最新会话进不了列表。
	pendingScan bool
	// scanned 标记本进程是否完成过一轮真实扫描（快照恢复不算）：
	// 绑定调用据此决定要不要等首扫，避免拿快照当真结果错过新工作区。
	scanned    bool
	quitting   bool // 托盘「退出」已发起：BeforeClose 据此放行 Wails 的退出流程
	trayActive bool // 系统托盘消息循环已启动：BeforeClose 据此判断能否收进托盘

	trees   map[string]*workspace.Tree // 工作区路径 → 文件树（懒创建，节点级缓存）
	treeMu  sync.Mutex                 // 树操作串行化（Expand 会写节点，不能只靠 mu 快照）
	treeGen uint64                     // 树缓存代数：rename 作废缓存时推进，防旧构建写回

	watchMu sync.Mutex                 // 文件监视表串行化
	watches map[string]*fileWatcher    // cleaned 工作区根 → 监视器（引用计数）

	appearanceCancel context.CancelFunc // system 模式下的明暗监听取消函数
}

// NewApp 创建绑定对象；真实依赖延迟到 Startup 装配（包级初始化时还拿不到用户目录）。
func NewApp() *App { return &App{} }

// NewAppWith 用给定选项创建绑定对象（测试注入用）。
func NewAppWith(o Options) *App { return &App{opts: o} }

// newWindowManagerWithEmit 创建带 onClosed→Emit 回调的窗口管理器：
// 回收死亡窗口后经回调推送 window:closed，让前端把对应行状态还原。
// 真实装配（initRealDeps）与测试共用这一构造路径，保证回调装配只写一处。
func newWindowManagerWithEmit(a *App, l TerminalLauncher) *WindowManager {
	return NewWindowManager(l, func(title string) { a.Emit("window:closed", title) })
}

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

	// 退出信号文件：启动时先消费一次残留（旧版本写入但没人监听、或应用
	// 退出瞬间外部脚本又写入的孤儿文件），否则新实例一启动就误判退出信号。
	if p := a.snapshot().SignalPath; p != "" {
		checkExitSignal(p)
		go a.watchExitSignal(p, exitSignalInterval)
	}

	a.loadSnapshot() // 先端出上次结果（秒开），真实扫描随后覆盖
	go a.runScan()
	go a.reapLoop()

	a.emitAppearance()
	a.restartAppearanceWatcher()
}

// loadSnapshot 把上次扫描的落盘快照灌进内存，让前端首次 GetWorkspaces/GetSessions
// 立即拿到数据（秒开），不必等目录遍历与文件解析。
// 只在本进程还没有结果时生效：已有结果说明后台扫描已经跑过一轮，不能被旧快照覆盖。
func (a *App) loadSnapshot() {
	o := a.snapshot()
	if o.SnapshotPath == "" {
		return
	}
	res, tools, err := discovery.LoadSnapshot(o.SnapshotPath)
	if err != nil || res == nil {
		return // 快照缺失/损坏/版本不符都无所谓：后台扫描会补上
	}

	raw := res.Workspaces
	res.Workspaces = discovery.ApplyProjects(raw, o.Projects, nil)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result != nil {
		return
	}
	a.result = res
	a.rawWorkspaces = raw
	if len(a.tools) == 0 && len(tools) > 0 {
		a.tools = tools
	}
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
	// 内置清单与 yaml 自定义定义合并（按 ID 去重、内置优先）。
	specs, _ := providers.LoadGenericSpecs(paths.Providers)
	ps := providers.MergeProviders(providers.Builtins(), specs, home)

	var wm *WindowManager
	if defaultLauncherFactory != nil {
		wm = newWindowManagerWithEmit(a, defaultLauncherFactory())
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
	if a.opts.Terminals == nil { // 幂等：已注入（测试）或已装配过就不重建，避免丢掉既有终端会话
		a.opts.Terminals = newTerminalManager(a)
	}
	if a.opts.Chats == nil { // 幂等：已注入（测试）或已装配过就不重建
		a.opts.Chats = newChatManager(a)
	}
	if a.opts.SignalPath == "" {
		a.opts.SignalPath = paths.SignalExit
	}
	if a.opts.SnapshotPath == "" {
		a.opts.SnapshotPath = paths.CacheSnapshot
	}
	if a.opts.ToolsCachePath == "" {
		a.opts.ToolsCachePath = paths.CacheTools
	}
	if a.opts.ProjectsPath == "" {
		a.opts.ProjectsPath = paths.Projects
	}
	if a.opts.Projects == nil {
		store := discovery.NewProjectStore(a.opts.ProjectsPath)
		_ = store.Load() // 读不到/损坏只当空表，不阻断启动（与连接存储同口径）
		a.opts.Projects = store
	}
	if a.opts.Layout.Config == "" {
		a.opts.Layout = paths
	}
	if a.opts.ThemeCacheDir == "" {
		a.opts.ThemeCacheDir = paths.CacheAppearance
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
	if len(icon) == 0 || ctx == nil {
		a.mu.Unlock()
		return
	}
	a.trayActive = true
	a.mu.Unlock()

	go runTrayFn(icon,
		func() { runtime.WindowShow(ctx) },
		func() { a.quitApp(ctx) },
	)
}

// runTrayFn 是 runTray 的包级变量抽象：测试注入用，避免单测真启动托盘消息循环。
var runTrayFn = runTray

// windowHide 是 runtime.WindowHide 的包级变量抽象：测试注入用，对齐 quitRuntime。
var windowHide = runtime.WindowHide

// quitRuntime 是 runtime.Quit 的包级变量抽象：测试注入用。
// Wails 的 runtime.Quit 在 ctx 不带 frontend 值时会 log.Fatalf 直接终止进程，
// 测试里必须替换掉才能安全验证「请求退出」这一步。
var quitRuntime = runtime.Quit

// quitApp 托盘「退出」入口：置退出标记并请求 Wails 退出进程。
// 注意 runtime.Quit 内部会再走一次 BeforeClose（见 wails windows frontend.Quit），
// quitting 标记保证那次调用放行，否则「退出」会被拦截成隐藏窗口、进程永远退不出去。
func (a *App) quitApp(ctx context.Context) {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	quitTrayLoop() // 先停托盘消息循环（通知区图标随之移除），再退进程
	quitRuntime(ctx)
}

// Shutdown 供 Wails OnShutdown 挂载：退出前关掉所有内嵌终端，避免残留子进程。
// 终端进程不跨进程存活，这里不需要（也无法）持久化。
func (a *App) Shutdown(ctx context.Context) {
	_ = ctx // 退出清理无取消语义：无论上下文如何都要把子进程收干净
	a.mu.Lock()
	if a.appearanceCancel != nil {
		a.appearanceCancel()
		a.appearanceCancel = nil
	}
	a.mu.Unlock()
	a.stopAllFileWatches()
	if m := a.snapshot().Terminals; m != nil {
		m.CloseAll()
	}
	if m := a.snapshot().Chats; m != nil {
		m.CloseAll()
	}
}

// BeforeClose 供 Wails OnBeforeClose 挂载：默认拦截窗口关闭改为隐藏到托盘（返回 true）。
// 放行（返回 false）的三种情形：托盘「退出」/重启/信号退出期间（quitting）、
// 关闭行为配置为 exit、或托盘未激活（避免窗口有去无回，直接退出）。
func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	behavior := a.opts.Config.CloseBehavior
	trayActive := a.trayActive
	a.mu.Unlock()

	if quitting {
		return false
	}
	if behavior == config.CloseBehaviorExit || !trayActive {
		return false
	}
	windowHide(ctx)
	return true
}

// GetCloseBehavior 返回归一化后的关闭行为：exit 原样返回，其余一律 tray（默认）。
func (a *App) GetCloseBehavior() string {
	a.mu.Lock()
	behavior := a.opts.Config.CloseBehavior
	a.mu.Unlock()
	if behavior == config.CloseBehaviorExit {
		return config.CloseBehaviorExit
	}
	return config.CloseBehaviorTray
}

// saveConfig 在 configWriteMu 保护下对内存配置做一次变更并落盘，成功后才提交回内存。
// 统一收口「读 → 改 → Save → 提交」，避免多个 setter 并发时丢失对方字段；
// 未装配 Layout 时返回 errNotReady。
func (a *App) saveConfig(mutate func(*config.Config)) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	a.mu.Lock()
	if a.opts.Layout.Config == "" {
		a.mu.Unlock()
		return errNotReady
	}
	layout := a.opts.Layout
	cfg := a.opts.Config
	a.mu.Unlock()

	mutate(&cfg)
	if err := config.Save(layout, cfg); err != nil {
		return err
	}

	a.mu.Lock()
	a.opts.Config = cfg
	a.mu.Unlock()
	return nil
}

// SetCloseBehavior 校验并持久化关闭行为，立即生效（无需重启）。
// 先写盘成功再提交内存，避免写失败却留下不一致；非法值或未装配 Layout 直接报错且不改状态。
func (a *App) SetCloseBehavior(mode string) error {
	if mode != config.CloseBehaviorTray && mode != config.CloseBehaviorExit {
		return fmt.Errorf("非法的关闭行为 %q（可选 tray / exit）", mode)
	}
	return a.saveConfig(func(cfg *config.Config) { cfg.CloseBehavior = mode })
}

// spawnSelf 启动应用自身新实例（包级变量便于测试注入）。
var spawnSelf = func(exe string) error {
	cmd := exec.Command(exe)
	executil.HideWindow(cmd) // 重启瞬间若父进程仍持有控制台句柄，避免新实例闪黑窗
	return cmd.Start()
}

// RestartApp 重启应用：启动新进程后走托盘退出路径（quitting 放行 BeforeClose）。
func (a *App) RestartApp(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := spawnSelf(exe); err != nil {
		return err
	}
	a.quitApp(ctx)
	return nil
}

// snapshot 加锁拷贝一份选项，调用方在锁外使用副本。
func (a *App) snapshot() Options {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts
}

// themeOptions 组装当前颜色模式与主题缓存目录，供 launch 注入。
func (a *App) themeOptions() launch.ThemeOptions {
	o := a.snapshot()
	return launch.ThemeOptions{
		Mode:     appearance.ParseMode(o.Config.Appearance.Mode),
		CacheDir: o.ThemeCacheDir,
	}
}

// modelOptions 组装模型注入 resolver：未启用时返回空（不注入）。
func (a *App) modelOptions() launch.ModelOptions {
	m := a.snapshot().Config.Model
	if !m.Enabled {
		return launch.ModelOptions{}
	}
	return launch.ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		return providers.ModelConfig{
			OpenAIBaseURL:    m.OpenAIBaseURL,
			AnthropicBaseURL: m.AnthropicBaseURL,
			APIKey:           m.APIKey,
			Model:            m.Agents[toolID],
		}, true
	}}
}

// permissionOptions 组装权限注入：bypass 时按工具追加跳过确认参数。
func (a *App) permissionOptions() launch.PermissionOptions {
	return launch.PermissionOptions{Bypass: a.snapshot().Config.PermissionMode == config.PermissionModeBypass}
}

// ScanSessions 触发一次会话扫描（已在扫则排队复扫），立即返回最近一次结果；
// 前端首次调用拿到 nil 属正常，等 "scan:done" 事件后再刷新。
// 未就绪（装配未完成）时返回 errNotReady 且不触发扫描。
func (a *App) ScanSessions() (*discovery.Result, error) {
	a.mu.Lock()
	res := a.result
	started := false
	if !a.opts.ready() {
		a.mu.Unlock()
		return res, errNotReady
	}
	if a.scanning {
		a.pendingScan = true // 本轮结束后自动再扫，避免新建会话的延迟重扫被丢掉
	} else {
		a.scanning = true
		started = true
	}
	a.mu.Unlock()

	if started {
		go a.runScan()
	}
	return res, nil
}

// runScan 执行一次扫描并推送 "scan:done" 事件（无论成败，前端据此刷新）。
// 失败时保留上一次成功结果，避免一次瞬时失败让已知会话/工作区全面失联。
// 若扫描期间又有 ScanSessions 请求（pendingScan），本轮结束后立即再扫一轮。
func (a *App) runScan() {
	o := a.snapshot()
	if !o.ready() {
		a.finishScan(false)
		a.Emit("scan:done", map[string]any{"failed": 0, "error": errNotReady.Error()})
		return
	}
	tools := discovery.DetectAllCached(o.Home, o.Providers, o.ToolsCachePath)
	res, err := o.Scan(o.Home, o.Providers, o.CachePath, discovery.ScanOptions{
		Roots:    o.Config.ScanRoots,
		MaxDepth: o.Config.MaxDepth,
		Exclude:  o.Config.Exclude,
	})

	// 项目表要叠加在「原始扫描结果」上：先记原始，再算给前端看的列表。
	// os.Stat 逐个判断目录存在性，放在锁外做。
	var raw []discovery.Workspace
	if err == nil && res != nil {
		raw = res.Workspaces
		res.Workspaces = discovery.ApplyProjects(raw, o.Projects, nil)
	}

	a.mu.Lock()
	// 上轮扫描已存在的会话 ID：只有「本轮新发现」的会话才可能对应运行中的新建终端/聊天
	// （新建动作发生在上轮扫描之后），否则工作区里任意旧会话都会被误绑。
	// 在锁内读取 a.result：runScan 是唯一写者，但读也必须持锁避免与其它 goroutine 竞争。
	prevIDs := make(map[string]bool)
	if a.result != nil {
		for _, s := range a.result.Sessions {
			prevIDs[s.ID] = true
		}
	}
	a.tools = tools
	if err == nil {
		a.result = res
		a.rawWorkspaces = raw
		a.scanned = true
	}
	a.mu.Unlock()

	// 落盘快照（锁外写文件）；失败只影响下次启动的秒开体验，不影响本次结果
	if err == nil {
		a.saveSnapshot(res, raw, tools)
	}

	// 扫描发现的会话回填到运行中的新建终端/聊天：新建时磁盘上还没有会话记录，
	// Info 只有占位标题、SessionID 为空；不回填则页签无法区分任务、「恢复/切换」判断失灵。
	// 已绑定的会话还会同步标题（首轮常为空，用户发消息后才有真实标题）。
	attached := 0
	if err == nil && res != nil {
		attached = a.attachDiscoveredSessions(res.Sessions, prevIDs)
	}

	ev := map[string]any{"failed": 0}
	if err != nil {
		ev["error"] = err.Error()
	}
	if res != nil {
		ev["sessions"] = res.Sessions
		ev["workspaces"] = res.Workspaces
		ev["failed"] = len(res.Failed)
	}
	if attached > 0 {
		ev["attached"] = true // 前端据此重取终端/聊天镜像
	}
	// 先推事件再决定是否复扫：前端能立刻吃到本轮结果；复扫在独立 goroutine，不挡事件。
	followUp := a.finishScan(true)
	a.Emit("scan:done", ev)
	if followUp {
		go a.runScan()
	}
}

// finishScan 结束本轮扫描标志。keepBusyOnPending 为 true 且有排队请求时保持 scanning，
// 由调用方立刻再启一轮；返回是否需要复扫。
func (a *App) finishScan(keepBusyOnPending bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	followUp := a.pendingScan
	a.pendingScan = false
	if followUp && keepBusyOnPending {
		// scanning 保持 true，避免间隙里第三方再开并行扫描
		return true
	}
	a.scanning = false
	return followUp
}

// attachDiscoveredSessions 把本轮扫描新发现的会话逐条尝试绑定到新建终端/聊天，
// 并对已绑定会话同步标题。返回发生绑定或标题更新的次数。
// prevIDs 是上轮扫描已存在的会话 ID 集合：其中的会话不再做首次绑定（见 runScan 内注释），
// 但仍走标题同步——首轮绑定时标题常为空，后续落盘后要能刷新页签。
func (a *App) attachDiscoveredSessions(sessions []providers.Session, prevIDs map[string]bool) int {
	o := a.snapshot()
	changed := 0
	for _, s := range sessions {
		if !prevIDs[s.ID] {
			if o.Terminals != nil && o.Terminals.AttachSession(s.ID, s.Workspace, s.ToolID, s.Title) {
				changed++
			}
			if o.Chats != nil && o.Chats.AttachSession(s.ID, s.Workspace, s.ToolID, s.Title) {
				changed++
			}
		}
		if s.Title == "" {
			continue
		}
		if o.Terminals != nil && o.Terminals.UpdateSessionTitle(s.ID, s.Title) {
			changed++
		}
		if o.Chats != nil && o.Chats.UpdateSessionTitle(s.ID, s.Title) {
			changed++
		}
	}
	return changed
}

// scanReadyTimeout 是「等首轮扫描结果」的上限。真实扫描通常几百毫秒，
// 超时只是兜底：结果始终没就绪就按不存在处理，不让绑定调用无限挂起。
const scanReadyTimeout = 3 * time.Second

// ensureScanReady 等待本进程的首轮真实扫描结果就绪：尚未扫完且依赖已装配时触发一轮
// 扫描（可能已在后台进行，不重复触发），轮询等待结果落位后返回。
// 已扫完（scanned）立即返回——本方法只兜底「应用刚启动还没扫完」，
// 不为「结果里确实没有」的场景再做全量扫描，避免无谓等待。
// 注意：启动时用快照灌入的 result 不算「已扫完」，否则新出现的工作区/会话会被判为不存在。
func (a *App) ensureScanReady() {
	deadline := time.Now().Add(scanReadyTimeout)
	for {
		a.mu.Lock()
		if (a.result != nil && a.scanned) || !a.opts.ready() {
			a.mu.Unlock()
			return
		}
		started := false
		if !a.scanning {
			a.scanning = true
			started = true
		}
		a.mu.Unlock()
		if started {
			go a.runScan()
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// GetWorkspaces 返回当前项目列表（未扫完时为空切片）。
// 每次都从**原始扫描结果**叠加项目表算出，因此「添加/删除/还原」后不必等重扫即可生效；
// 派生结果每次都是新切片，调用方可以放心改。
func (a *App) GetWorkspaces() []discovery.Workspace {
	a.mu.Lock()
	raw, st := a.rawWorkspaces, a.opts.Projects
	a.mu.Unlock()
	return discovery.ApplyProjects(raw, st, nil)
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
	s, tools, ok := a.sessionByIDReady(id)
	if !ok {
		return errSessionNotFound
	}
	o := a.snapshot()
	l, err := launch.ForSession(o.Providers, tools, s, a.themeOptions(), a.modelOptions(), a.permissionOptions())
	if err != nil {
		return err
	}
	return a.launchWindow(o.Windows, l, s.Title)
}

// NewSession 在指定工作区新建会话（wsID 为工作区路径）。
// 工具交给 launch 选工作区首选（等价于 NewSessionWithTool 的 toolID 为空）。
func (a *App) NewSession(wsID string) error {
	return a.NewSessionWithTool(wsID, "")
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
	return wm.LaunchSession(l.Dir, titleText, psStatement(l.Path, l.Args, l.Env))
}

// psStatement 生成在已打开的 PowerShell 窗口里执行 CLI 的语句：先设 $env: 变量，再
// & 'bin' 'arg1'（调用运算符 + 单引号字面量，内嵌单引号双写转义）。
func psStatement(bin string, args []string, env map[string]string) string {
	s := ""
	for _, kv := range providers.EnvList(env) {
		i := strings.IndexByte(kv, '=')
		s += "$env:" + kv[:i] + " = " + psQuote(kv[i+1:]) + "; "
	}
	s += "& " + psQuote(bin)
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

// workspaceByID 按路径查工作区。匹配走 discovery.NormalizePath 归一化：
// 前端页签 id 持久化的是上次扫描时 Workspace.Path 的原始形态，而每次扫描
// Path 取自「第一个出现的会话 cwd」——盘符大小写/分隔符一变（D:\ 与 d:\、
// \ 与 /），严格相等就会误报「工作区不存在」，必须与聚合 key 同口径比较。
func (a *App) workspaceByID(id string) (discovery.Workspace, []discovery.Tool, bool) {
	want := discovery.NormalizePath(id)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result != nil {
		for _, ws := range a.result.Workspaces {
			if discovery.NormalizePath(ws.Path) == want {
				return ws, a.tools, true
			}
		}
	}
	return discovery.Workspace{}, nil, false
}

// workspaceByIDReady 在 workspaceByID 基础上兜底「首轮扫描未完成」：
// 查不到时等一轮扫描（最多 scanReadyTimeout）再查一次，避免应用刚启动、
// 前端持久化页签立即点新建/终端时误报工作区不存在。
func (a *App) workspaceByIDReady(id string) (discovery.Workspace, []discovery.Tool, bool) {
	if ws, tools, ok := a.workspaceByID(id); ok {
		return ws, tools, true
	}
	a.ensureScanReady()
	return a.workspaceByID(id)
}

// sessionByIDReady 在 sessionByID 基础上兜底「首轮扫描未完成」，语义同 workspaceByIDReady。
func (a *App) sessionByIDReady(id string) (providers.Session, []discovery.Tool, bool) {
	if s, tools, ok := a.sessionByID(id); ok {
		return s, tools, true
	}
	a.ensureScanReady()
	return a.sessionByID(id)
}

// Emit 对外转发事件（窗口关闭回调等内部使用；未就绪时静默丢弃）。
func (a *App) Emit(name string, data ...any) {
	emit := a.snapshot().Emit
	if emit != nil {
		emit(name, data...)
	}
}
