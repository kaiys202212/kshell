package ui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/workspace"
)

type ViewID int

const (
	ViewSessions ViewID = iota
	ViewFiles
	ViewRemote
	viewCount
)

var viewNames = []string{"Sessions", "Files", "Remote"}

// 小屏折叠阈值：低于此尺寸改为纵向堆叠，避免三区挤成一团。
const (
	minSplitWidth  = 80
	minSplitHeight = 24
)

type Options struct {
	Home      string
	Config    config.Config
	Providers []providers.Provider
	CachePath string
	Store     *remote.Store
	Scanners  []remote.Scanner
}

var (
	errNoHome              = errors.New("无法确定用户主目录")
	errNoSessionSelected   = errors.New("没有选中的会话")
	errNoWorkspaceSelected = errors.New("没有选中的工作区")
)

type statusMsg struct {
	text string
	warn bool
}

type previewMsg struct {
	key     string
	preview workspace.Preview
}

// sessionEndedMsg 在 agent CLI 退出后触发一次重扫，让新建的会话立刻出现在列表里。
type sessionEndedMsg struct {
	text string
	warn bool
}

type scanDoneMsg struct {
	tools      []discovery.Tool
	workspaces []discovery.Workspace
	sessions   []providers.Session
	failed     int
	err        error
}

type Model struct {
	width  int
	height int
	view   ViewID
	theme  Theme
	opts   Options

	tools      []discovery.Tool
	workspaces []discovery.Workspace
	sessions   []providers.Session

	wsCursor   int
	sessCursor int
	focus      string
	filter     string
	filtering  bool

	status     string
	statusWarn bool
	failed     int
	loading    bool
	showHelp   bool
	basket     []string // 上下文篮：文件视图里勾选的文件，新建会话时注入

	tree         *workspace.Tree
	treeRootPath string
	treeShowAll  bool
	showAll      bool
	fileCursor   int
	lastFileErr  error

	store        *remote.Store
	conns        []remote.Connection
	connCursor   int
	candidates   []remote.Candidate
	candCursor   int
	candChecked  map[string]bool
	importing    bool
	cmdInput     string
	cmdInputting bool
	output       []string
	execRunning  bool

	previewKey     string
	preview        workspace.Preview
	previewPending bool
	previewScroll  int
}

func NewModel() Model {
	return NewModelWith(Options{})
}

func NewModelWith(o Options) Model {
	if len(o.Providers) == 0 {
		o.Providers = []providers.Provider{providers.Claude{}, providers.Codex{}, providers.Gemini{}}
	}
	if o.Config.MaxDepth <= 0 {
		o.Config = config.Default()
	}

	return Model{
		width:  120,
		height: 40,
		view:   ViewSessions,
		theme:  NewTheme(),
		opts:   o,
		store:  o.Store,
		focus:  focusWorkspaces,
		status: "就绪",
	}
}

// WithStatus 设置状态栏文案（warn=true 时用醒目样式）。
func (m Model) WithStatus(text string, warn bool) Model {
	m.status, m.statusWarn = text, warn
	return m
}

// Init 启动时后台扫描，先渲染空面板再补数据，UI 不阻塞。
func (m Model) Init() tea.Cmd {
	return scanCmd(m.opts)
}

func scanCmd(o Options) tea.Cmd {
	return func() tea.Msg {
		if o.Home == "" {
			return scanDoneMsg{err: errNoHome}
		}
		tools := discovery.DetectAll(o.Home, o.Providers)
		res, err := discovery.Scan(o.Home, o.Providers, o.CachePath, discovery.ScanOptions{
			Roots:    o.Config.ScanRoots,
			MaxDepth: o.Config.MaxDepth,
			Exclude:  o.Config.Exclude,
		})
		if err != nil {
			return scanDoneMsg{tools: tools, err: err}
		}
		return scanDoneMsg{
			tools:      tools,
			workspaces: res.Workspaces,
			sessions:   res.Sessions,
			failed:     len(res.Failed),
		}
	}
}

func statusCmd(text string, warn bool) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text, warn: warn} }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.ensureTree()
	m.ensureConns()

	model, cmd := m.update(msg)
	next, ok := model.(Model)
	if !ok {
		return model, cmd
	}
	// 文件预览走异步加载：渲染路径上绝不读盘
	if pc := next.ensurePreview(); pc != nil {
		cmd = tea.Batch(cmd, pc)
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		return m, nil

	case scanDoneMsg:
		m.loading = false
		m.tools = msg.tools
		m.workspaces = msg.workspaces
		m.sessions = msg.sessions
		m.failed = msg.failed
		m.clampCursors()
		switch {
		case msg.err != nil:
			m.status, m.statusWarn = "扫描失败："+msg.err.Error(), true
		case msg.failed > 0:
			m.status, m.statusWarn = "扫描完成，"+itoa(msg.failed)+" 个会话解析失败", true
		default:
			m.status, m.statusWarn = "扫描完成，共 "+itoa(len(m.sessions))+" 个会话", false
		}
		return m, nil

	case candidatesMsg:
		m.loading = false
		m.importing = true
		m.candidates = msg.cands
		m.candCursor = 0
		m.candChecked = map[string]bool{} // 低置信度默认不勾选，由用户确认
		if msg.err != nil {
			m.status, m.statusWarn = "扫描失败："+msg.err.Error(), true
		} else if len(msg.cands) == 0 {
			m.status, m.statusWarn = "没有扫到候选连接", false
		} else {
			m.status, m.statusWarn = "扫到 "+itoa(len(msg.cands))+" 个候选，空格勾选后回车导入", false
		}
		return m, nil

	case connsMsg:
		m.importing = false
		m.candidates = nil
		m.candChecked = map[string]bool{}
		m.ensureConns()
		if msg.err != nil {
			m.status, m.statusWarn = "导入失败："+msg.err.Error(), true
			return m, nil
		}
		text := "已导入 " + itoa(msg.imported) + " 条连接"
		warn := false
		if msg.skipped > 0 {
			text += "，跳过重复 " + itoa(msg.skipped)
		}
		if msg.failed > 0 {
			text += "，失败 " + itoa(msg.failed)
			warn = true
		}
		m.status, m.statusWarn = text, warn
		return m, nil

	case previewMsg:
		if msg.key == m.previewKey {
			m.preview = msg.preview
			m.previewPending = false
		}
		return m, nil

	case sessionEndedMsg:
		m.status, m.statusWarn = msg.text, msg.warn
		return m, scanCmd(m.opts) // 新建的会话要能立刻看到

	case execDoneMsg:
		m.execRunning = false
		if msg.err != nil {
			m.status, m.statusWarn = "执行失败："+msg.err.Error(), true
			m.output = append(m.output, m.theme.Header.Render("错误 "+msg.err.Error()))
			return m, nil
		}
		m.status, m.statusWarn = msg.command+" （退出码 "+itoa(msg.res.ExitCode)+"）", msg.res.ExitCode != 0
		if msg.command == "true" && msg.connID != "" && msg.res.ExitCode == 0 {
			m.markVerified(msg.connID) // 连通性测试通过 → 点亮 ✓
		}
		m.output = append(m.output, m.theme.Body.Render("$ "+msg.command))
		m.output = append(m.output, splitLines(msg.res.Stdout)...)
		if strings.TrimSpace(msg.res.Stderr) != "" {
			m.output = append(m.output, splitLines(msg.res.Stderr)...)
		}
		if len(m.output) > 200 {
			m.output = m.output[len(m.output)-200:]
		}
		return m, nil

	case statusMsg:
		m.status, m.statusWarn = msg.text, msg.warn
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.cmdInputting {
		switch key {
		case "esc":
			m.cmdInputting = false
			m.cmdInput = ""
			return m, nil
		case "enter":
			command := strings.TrimSpace(m.cmdInput)
			m.cmdInputting = false
			m.cmdInput = ""
			if command == "" {
				return m, nil
			}
			m.execRunning = true
			return m, m.execRemoteCmd(command)
		case "backspace":
			if len(m.cmdInput) > 0 {
				m.cmdInput = m.cmdInput[:len(m.cmdInput)-1]
			}
			return m, nil
		}
		if len(key) == 1 {
			m.cmdInput += key
		}
		return m, nil
	}

	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filter = ""
			m.clampCursors()
			return m, nil
		case "enter":
			m.filtering = false
			m.clampCursors()
			return m, nil
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.clampCursors()
			}
			return m, nil
		}
		if len(key) == 1 {
			m.filter += key
			m.clampCursors()
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.view = ViewID((int(m.view) + 1) % int(viewCount))
		return m, nil
	case "1", "2", "3":
		m.view = ViewID(int(key[0] - '1'))
		return m, nil
	case "/":
		m.filtering = true
		return m, nil
	case "r":
		m.loading = true
		m.status, m.statusWarn = "正在扫描…", false
		return m, scanCmd(m.opts)
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "esc":
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		m.focus = focusWorkspaces
		return m, nil
	case "a":
		if m.view == ViewFiles {
			m.toggleShowAll()
			return m, nil
		}
	case "pgup":
		if m.view == ViewFiles {
			m.previewScroll -= 10
			if m.previewScroll < 0 {
				m.previewScroll = 0
			}
			return m, nil
		}
	case "pgdown":
		if m.view == ViewFiles {
			m.previewScroll += 10
			return m, nil
		}
	case " ":
		if m.view == ViewFiles {
			if row, ok := m.selectedFile(); ok && !row.Node.IsDir {
				m.toggleBasket(row.Node.Path)
			}
			return m, nil
		}
		if m.view == ViewRemote && m.importing {
			m.toggleCandidate()
			return m, nil
		}
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "i":
		if m.view == ViewRemote {
			m.loading = true
			m.status, m.statusWarn = "正在扫描连接…", false
			return m, m.scanCandidatesCmd()
		}
	case "x":
		if m.view == ViewRemote {
			m.cmdInputting = true
			m.cmdInput = ""
			return m, nil
		}
	case "s":
		if m.view == ViewRemote {
			return m, m.shellCmd()
		}
	case "t":
		if m.view == ViewRemote {
			m.execRunning = true
			return m, m.testConnCmd()
		}
	case "d":
		if m.view == ViewRemote {
			m.deleteSelectedConn()
			return m, nil
		}
	case "b":
		if m.view == ViewRemote {
			m.bindSelectedConn()
			return m, nil
		}
	case "enter":
		if m.view == ViewFiles {
			m.toggleDir()
			return m, nil
		}
		if m.view == ViewRemote {
			if m.importing {
				return m, m.importCheckedCmd()
			}
			return m, nil
		}
		if m.focus == focusWorkspaces {
			m.focus = focusSessions
			m.sessCursor = 0
			return m, nil
		}
		return m, m.launchSelectedCmd()
	case "n":
		return m, m.launchNewCmd()
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	switch m.view {
	case ViewFiles:
		m.fileCursor += delta
		if m.fileCursor < 0 {
			m.fileCursor = 0
		}
	case ViewRemote:
		if m.importing {
			m.candCursor += delta
			if m.candCursor < 0 {
				m.candCursor = 0
			}
		} else {
			m.connCursor += delta
			if m.connCursor < 0 {
				m.connCursor = 0
			}
		}
	default:
		if m.focus == focusWorkspaces {
			m.wsCursor += delta
			if m.wsCursor < 0 {
				m.wsCursor = 0
			}
		} else {
			m.sessCursor += delta
			if m.sessCursor < 0 {
				m.sessCursor = 0
			}
		}
	}
	m.clampCursors()
}

func (m *Model) clampCursors() {
	if n := len(m.visibleWorkspaces()); n > 0 && m.wsCursor >= n {
		m.wsCursor = n - 1
	}
	if m.wsCursor < 0 {
		m.wsCursor = 0
	}
	if n := len(m.sessionsOfSelected()); n > 0 && m.sessCursor >= n {
		m.sessCursor = n - 1
	}
	if m.sessCursor < 0 {
		m.sessCursor = 0
	}
	if n := len(m.fileRows()); n > 0 && m.fileCursor >= n {
		m.fileCursor = n - 1
	}
	if m.fileCursor < 0 {
		m.fileCursor = 0
	}
	if n := len(m.conns); n > 0 && m.connCursor >= n {
		m.connCursor = n - 1
	}
	if m.connCursor < 0 {
		m.connCursor = 0
	}
	if n := len(m.candidates); n > 0 && m.candCursor >= n {
		m.candCursor = n - 1
	}
	if m.candCursor < 0 {
		m.candCursor = 0
	}
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// resumeLaunch 给出恢复选中会话所需的启动描述；解析逻辑下沉在 launch 包，UI 与桌面版共用。
func (m Model) resumeLaunch() (providers.Launch, error) {
	s, ok := m.selectedSession()
	if !ok {
		return providers.Launch{}, errNoSessionSelected
	}
	return launch.ForSession(m.opts.Providers, m.tools, s)
}

func (m Model) newSessionLaunch() (providers.Launch, error) {
	ws, ok := m.selectedWorkspace()
	if !ok {
		return providers.Launch{}, errNoWorkspaceSelected
	}
	return launch.ForWorkspace(m.opts.Providers, m.tools, ws, m.basket)
}

func (m Model) launchSelectedCmd() tea.Cmd {
	if m.view != ViewSessions {
		return nil
	}
	launch, err := m.resumeLaunch()
	if err != nil {
		return statusCmd(err.Error(), true)
	}
	spec, err := launcher.Build(launch)
	if err != nil {
		return statusCmd(err.Error(), true)
	}
	return execSession(m, spec, "已结束会话")
}

func (m Model) launchNewCmd() tea.Cmd {
	if m.view != ViewSessions && m.view != ViewFiles {
		return nil
	}
	launch, err := m.newSessionLaunch()
	if err != nil {
		return statusCmd(err.Error(), true)
	}
	spec, err := launcher.Build(launch)
	if err != nil {
		return statusCmd(err.Error(), true)
	}
	return execSession(m, spec, "已退出新建会话")
}

// execSession 把终端交给 agent CLI，退出后回到 kshell 并触发一次重扫（新会话要能立刻看到）。
func execSession(m Model, spec launcher.Spec, doneMsg string) tea.Cmd {
	cmd := spec.Cmd(context.Background())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return sessionEndedMsg{text: "会话退出异常：" + err.Error(), warn: true}
		}
		return sessionEndedMsg{text: doneMsg}
	})
}

func (m Model) View() string {
	top := m.renderTopBar()
	bottom := m.renderStatusBar()

	bodyHeight := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	if m.showHelp {
		body = renderHelp(bodyHeight, m.width, m.theme)
	} else if m.width < minSplitWidth || m.height < minSplitHeight {
		if bodyHeight < 2 {
			body = m.renderRight(bodyHeight, m.width)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left,
				m.renderLeft(bodyHeight/2, m.width),
				m.renderRight(bodyHeight-bodyHeight/2, m.width),
			)
		}
	} else {
		leftWidth := m.width * 40 / 100
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderLeft(bodyHeight, leftWidth),
			m.renderRight(bodyHeight, m.width-leftWidth),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

func (m Model) renderLeft(height, width int) string {
	switch m.view {
	case ViewFiles:
		return m.renderFilesBody(height, width)
	case ViewRemote:
		return m.renderRemoteBody(height, width)
	default:
		return m.renderSessionsBody(height, width)
	}
}

func (m Model) renderRight(height, width int) string {
	switch m.view {
	case ViewFiles:
		return m.renderFilesPreview(height, width)
	case ViewRemote:
		return m.renderRemoteRight(height, width)
	default:
		return m.renderSessionPreview(height, width)
	}
}

func (m Model) renderTopBar() string {
	left := m.theme.Title.Render(" kshell ") + " " + m.renderTabs() + " " + m.renderToolChips()
	if len(m.basket) > 0 {
		left += " " + m.theme.TabActive.Render("篮 "+itoa(len(m.basket)))
	}
	right := m.theme.Muted.Render(m.workspaceHeader())
	return joinHorizontalFit(m.width, left, right)
}

func (m Model) workspaceHeader() string {
	ws, ok := m.selectedWorkspace()
	if !ok {
		return "ws: -"
	}
	return "ws: " + ws.Path
}

func (m Model) renderToolChips() string {
	chips := make([]string, 0, len(m.tools))
	for _, t := range m.tools {
		if t.Installed {
			chips = append(chips, m.theme.TabActive.Render("●"+t.ID))
			continue
		}
		chips = append(chips, m.theme.Muted.Render("○"+t.ID))
	}
	return strings.Join(chips, " ")
}

func (m Model) renderTabs() string {
	tabs := make([]string, 0, len(viewNames))
	for i, name := range viewNames {
		if ViewID(i) == m.view {
			tabs = append(tabs, m.theme.TabActive.Render("["+name+"]"))
			continue
		}
		tabs = append(tabs, m.theme.Tab.Render(name))
	}
	return strings.Join(tabs, " ")
}

func (m Model) renderStatusBar() string {
	if m.cmdInputting {
		return fitBlock([]string{m.theme.Body.Render("ssh> " + m.cmdInput + "_")}, 1, m.width)
	}
	if m.filtering {
		return fitBlock([]string{m.theme.Body.Render("/" + m.filter + "_")}, 1, m.width)
	}

	hint := "↑↓ 移动  ⏎ 进入  / 搜索  n 新建  r 重扫  ? 帮助  q 退出"
	if m.view == ViewRemote {
		hint = m.renderRemoteStatusLine()
	}
	return joinHorizontalFit(m.width, m.theme.Muted.Render(hint), m.statusStyle().Render(m.status))
}

func (m Model) statusStyle() lipgloss.Style {
	if m.statusWarn {
		return m.theme.Header
	}
	return m.theme.StatusBar
}

// joinHorizontalFit 把左右两段拼成恰好 width 列的一行；放不下时优先保留右侧，左侧截断加省略号。
func joinHorizontalFit(width int, left, right string) string {
	if width <= 0 {
		return ""
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)

	if leftWidth+rightWidth+1 > width {
		avail := width - rightWidth - 1
		if avail > 0 {
			left = ansi.Truncate(left, avail, "…")
			leftWidth = lipgloss.Width(left)
		}
	}

	gap := width - leftWidth - rightWidth
	if gap < 1 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitBlock 把若干行补齐并截断到指定高宽，保证 View() 每行宽度都不超过可用列数。
// 只补不截会让超长行把右栏挤出屏幕，所以这里必须双向处理。
func fitBlock(lines []string, height, width int) string {
	if height < 0 {
		height = 0
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		s := ""
		if i < len(lines) {
			s = lines[i]
		}
		if width > 0 {
			s = ansi.Truncate(s, width, "…")
		}
		out[i] = padRight(s, width)
	}
	return strings.Join(out, "\n")
}

func padRight(s string, width int) string {
	if width <= 0 {
		return s
	}
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
