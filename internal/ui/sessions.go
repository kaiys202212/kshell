package ui

import (
	"strings"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

const (
	focusWorkspaces = "workspaces"
	focusSessions   = "sessions"
)

// visibleWorkspaces 返回工作区列表。过滤只作用在「当前聚焦的区块」上：
// 在会话里搜索时若把工作区也过滤掉，就再也选不到别的工作区了。
func (m Model) visibleWorkspaces() []discovery.Workspace {
	filter := strings.ToLower(strings.TrimSpace(m.filter))
	if filter == "" || m.focus != focusWorkspaces {
		return m.workspaces
	}

	out := make([]discovery.Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		if strings.Contains(strings.ToLower(w.Path), filter) {
			out = append(out, w)
		}
	}
	return out
}

// sessionsOfSelected 返回选中工作区下的会话，并叠加过滤条件。
func (m Model) sessionsOfSelected() []providers.Session {
	ws, ok := m.selectedWorkspace()
	if !ok {
		return nil
	}

	filter := strings.ToLower(strings.TrimSpace(m.filter))
	out := make([]providers.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if discovery.NormalizePath(s.Workspace) != discovery.NormalizePath(ws.Path) {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(s.Title), filter) &&
			!strings.Contains(strings.ToLower(s.ID), filter) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (m Model) selectedWorkspace() (discovery.Workspace, bool) {
	list := m.visibleWorkspaces()
	if len(list) == 0 {
		return discovery.Workspace{}, false
	}
	if m.wsCursor >= len(list) {
		return discovery.Workspace{}, false
	}
	return list[m.wsCursor], true
}

func (m Model) selectedSession() (providers.Session, bool) {
	list := m.sessionsOfSelected()
	if len(list) == 0 {
		return providers.Session{}, false
	}
	if m.sessCursor >= len(list) {
		return providers.Session{}, false
	}
	return list[m.sessCursor], true
}

func (m Model) renderSessionsBody(height, width int) string {
	listHeight := height / 2
	sessHeight := height - listHeight
	if height < 4 {
		listHeight, sessHeight = height, 0
	}

	workspaces := m.visibleWorkspaces()
	wsLines := make([]string, 0, listHeight)
	wsLines = append(wsLines, m.sectionHeader("WORKSPACES", focusWorkspaces))
	for i, w := range workspaces {
		if i >= listHeight-2 {
			break
		}
		wsLines = append(wsLines, m.renderRow(i == m.wsCursor && m.focus == focusWorkspaces, workspaceLabel(w)))
	}
	if len(workspaces) == 0 {
		wsLines = append(wsLines, m.theme.Muted.Render("（没有匹配的工作区）"))
	}

	sessionLines := make([]string, 0, sessHeight)
	sessionLines = append(sessionLines, m.sectionHeader("SESSIONS", focusSessions))
	for i, s := range m.sessionsOfSelected() {
		if i >= sessHeight-2 {
			break
		}
		sessionLines = append(sessionLines, m.renderRow(i == m.sessCursor && m.focus == focusSessions, sessionLabel(s)))
	}
	if len(m.sessionsOfSelected()) == 0 {
		sessionLines = append(sessionLines, m.theme.Muted.Render("（该工作区下没有匹配的会话）"))
	}

	left := fitBlock(wsLines, listHeight, width)
	right := fitBlock(sessionLines, sessHeight, width)
	if sessHeight == 0 {
		return left
	}
	return left + "\n" + right
}

func (m Model) renderSessionPreview(height, width int) string {
	lines := []string{m.sectionHeader("PREVIEW", "")}

	s, ok := m.selectedSession()
	if !ok {
		lines = append(lines, "", m.theme.Muted.Render("选中左侧会话查看摘要"))
		return fitBlock(lines, height, width)
	}

	lines = append(lines,
		m.theme.Body.Render(s.Title),
		"",
		m.theme.Muted.Render("工具   "+s.ToolID),
		m.theme.Muted.Render("工作区 "+s.Workspace),
		m.theme.Muted.Render("更新   "+s.UpdatedAt.Format("2006-01-02 15:04")),
		m.theme.Muted.Render("消息   "+itoa(s.Messages)),
		m.theme.Muted.Render("ID     "+s.ID),
	)
	return fitBlock(lines, height, width)
}

func (m Model) renderRow(selected bool, text string) string {
	if selected {
		return m.theme.TabActive.Render("▸ " + text)
	}
	return m.theme.Body.Render("  " + text)
}

func (m Model) sectionHeader(title, focusKey string) string {
	if focusKey != "" && m.focus == focusKey {
		return m.theme.Header.Render(title + " *")
	}
	return m.theme.Header.Render(title)
}

func workspaceLabel(w discovery.Workspace) string {
	label := w.Name + "  " + itoa(w.SessionCount)
	if w.Source == "git" {
		label += "  (git)"
	}
	return label
}

func sessionLabel(s providers.Session) string {
	title := s.Title
	if title == "" {
		title = "(无标题会话)"
	}
	return title + "  " + relTime(s.UpdatedAt) + "  " + s.ToolID
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
