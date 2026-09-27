package ui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

type Model struct {
	width     int
	height    int
	view      ViewID
	workspace string
	status    string
	theme     Theme
}

func NewModel() Model {
	return Model{
		width:     120,
		height:    40,
		view:      ViewSessions,
		workspace: "-",
		status:    "就绪",
		theme:     NewTheme(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.view = ViewID((int(m.view) + 1) % int(viewCount))
			return m, nil
		case "1", "2", "3":
			m.view = ViewID(int(msg.String()[0] - '1'))
			return m, nil
		}
	}
	return m, nil
}

func (m Model) View() string {
	top := m.renderTopBar()
	bottom := m.renderStatusBar()

	bodyHeight := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	if m.width < minSplitWidth || m.height < minSplitHeight {
		// 极矮终端：只留预览，避免空块让 JoinVertical 多算一行把顶栏挤出屏幕。
		if bodyHeight < 2 {
			body = m.renderPreview(bodyHeight, m.width)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left,
				m.renderList(bodyHeight/2, m.width),
				m.renderPreview(bodyHeight-bodyHeight/2, m.width),
			)
		}
	} else {
		leftWidth := m.width * 40 / 100
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderList(bodyHeight, leftWidth),
			m.renderPreview(bodyHeight, m.width-leftWidth),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

func (m Model) renderTopBar() string {
	left := m.theme.Title.Render(" kshell ") + " " + m.renderTabs()
	right := m.theme.Muted.Render("ws: " + m.workspace)
	return joinHorizontalFit(m.width, left, right)
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
	hint := "↑↓ 移动  ⏎ 进入  / 搜索  n 新建  r 重扫  ? 帮助  q 退出"
	return joinHorizontalFit(m.width, m.theme.Muted.Render(hint), m.theme.StatusBar.Render(m.status))
}

func (m Model) renderList(height, width int) string {
	lines := []string{
		m.theme.Header.Render("WORKSPACES"),
		"",
		m.theme.Body.Render("(等待扫描…)"),
		"",
		m.theme.Header.Render("SESSIONS"),
	}
	return fitBlock(lines, height, width)
}

func (m Model) renderPreview(height, width int) string {
	lines := []string{
		m.theme.Header.Render("PREVIEW"),
		"",
		m.theme.Preview.Render("选中左侧条目查看详情"),
	}
	return fitBlock(lines, height, width)
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
