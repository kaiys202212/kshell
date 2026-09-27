package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ViewID int

const (
	ViewSessions ViewID = iota
	ViewFiles
	ViewRemote
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
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.view = ViewID((int(m.view) + 1) % len(viewNames))
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
		// 小屏：列表与预览纵向堆叠，宽度只受终端列数约束。
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.renderList(bodyHeight/2, m.width),
			m.renderPreview(bodyHeight-bodyHeight/2, m.width),
		)
	} else {
		leftWidth := m.width * 40 / 100
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderList(bodyHeight, leftWidth),
			m.renderPreview(bodyHeight, m.width-leftWidth),
		)
	}

	out := lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
	// 兜底：任何一行都不得超出终端列数（提示文案过长时按列宽截断）。
	return lipgloss.NewStyle().MaxWidth(m.width).Render(out)
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
		m.theme.Muted.Render("(等待扫描…)"),
		"",
		m.theme.Header.Render("SESSIONS"),
	}
	return fitBlock(lines, height, width)
}

func (m Model) renderPreview(height, width int) string {
	lines := []string{
		m.theme.Header.Render("PREVIEW"),
		"",
		m.theme.Muted.Render("选中左侧条目查看详情"),
	}
	return fitBlock(lines, height, width)
}

func joinHorizontalFit(width int, left, right string) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitBlock 把若干行补齐/截断到指定高宽，保证 View() 输出的尺寸稳定。
func fitBlock(lines []string, height, width int) string {
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, padRight(lines[i], width))
			continue
		}
		out = append(out, padRight("", width))
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
