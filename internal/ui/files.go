package ui

import (
	"strings"

	"github.com/yangk/kshell/internal/workspace"
)

const maxBasket = 20

// ensureTree 为当前选中的工作区构建文件树；切换工作区或开关 showAll 时重建。
func (m *Model) ensureTree() {
	ws, ok := m.selectedWorkspace()
	if !ok {
		m.tree, m.treeRootPath = nil, ""
		return
	}
	if m.tree != nil && m.treeRootPath == ws.Path && m.treeShowAll == m.showAll {
		return
	}

	matcher := workspace.NewMatcher(ws.Path, m.opts.Config.Exclude, m.showAll)
	tree := workspace.NewTree(ws.Path, matcher)
	if err := tree.Expand(tree.Root); err != nil {
		m.lastFileErr = err
		m.tree, m.treeRootPath = nil, ""
		return
	}

	m.lastFileErr = nil
	m.tree = tree
	m.treeRootPath = ws.Path
	m.treeShowAll = m.showAll
	m.fileCursor = 0
}

func (m Model) fileRows() []workspace.Row {
	if m.tree == nil {
		return nil
	}
	return m.tree.Visible()
}

func (m Model) selectedFile() (workspace.Row, bool) {
	rows := m.fileRows()
	if len(rows) == 0 {
		return workspace.Row{}, false
	}
	if m.fileCursor >= len(rows) {
		return workspace.Row{}, false
	}
	return rows[m.fileCursor], true
}

// toggleBasket 把当前文件加入/移出上下文篮（去重、上限 maxBasket）。
func (m *Model) toggleBasket(path string) {
	if path == "" {
		return
	}
	for i, p := range m.basket {
		if p == path {
			m.basket = append(m.basket[:i], m.basket[i+1:]...)
			return
		}
	}
	if len(m.basket) >= maxBasket {
		m.status, m.statusWarn = "上下文篮已满（"+itoa(maxBasket)+" 个）", true
		return
	}
	m.basket = append(m.basket, path)
}

func (m Model) inBasket(path string) bool {
	for _, p := range m.basket {
		if p == path {
			return true
		}
	}
	return false
}

func (m *Model) toggleShowAll() {
	m.showAll = !m.showAll
	m.tree = nil // 强制重建
	m.ensureTree()
	if m.showAll {
		m.status, m.statusWarn = "已显示全部文件（含忽略项）", false
	} else {
		m.status, m.statusWarn = "已恢复忽略规则", false
	}
}

// expandOrEnter 展开/折叠目录；文件不做处理。
func (m *Model) toggleDir() {
	row, ok := m.selectedFile()
	if !ok || !row.Node.IsDir || m.tree == nil {
		return
	}
	if err := m.tree.Toggle(row.Node); err != nil {
		m.lastFileErr = err
		m.status, m.statusWarn = "无法展开目录："+err.Error(), true
	}
}

func (m Model) renderFilesBody(height, width int) string {
	lines := []string{m.sectionHeader("FILES", "")}

	if m.tree == nil {
		lines = append(lines, "", m.theme.Muted.Render("（没有选中的工作区）"))
		return fitBlock(lines, height, width)
	}

	rows := m.fileRows()
	for i, row := range rows {
		if len(lines) >= height-1 {
			break
		}
		prefix := strings.Repeat("  ", row.Depth)
		name := row.Node.Name
		if row.Node.IsDir {
			name = prefix2(row.Node.Expanded) + name + "/"
		} else {
			name = "  " + name
		}
		if m.inBasket(row.Node.Path) {
			name += " ✓"
		}
		lines = append(lines, m.renderRow(i == m.fileCursor, prefix+name))
	}
	if len(rows) == 0 {
		lines = append(lines, m.theme.Muted.Render("（空目录或全部被忽略）"))
	}
	return fitBlock(lines, height, width)
}

func prefix2(expanded bool) string {
	if expanded {
		return "▾ "
	}
	return "▸ "
}

func (m Model) renderFilesPreview(height, width int) string {
	lines := []string{m.sectionHeader("PREVIEW", "")}

	row, ok := m.selectedFile()
	if !ok {
		lines = append(lines, "", m.theme.Muted.Render("选中文件查看内容"))
		return fitBlock(lines, height, width)
	}

	p := workspace.PreviewFile(row.Node.Path, 0, height-3)
	if p.Err != nil {
		lines = append(lines, "", m.theme.Muted.Render("读取失败："+p.Err.Error()))
		return fitBlock(lines, height, width)
	}
	if p.Binary {
		lines = append(lines, "", m.theme.Muted.Render("二进制文件"), m.theme.Muted.Render(p.Info))
		return fitBlock(lines, height, width)
	}

	lines = append(lines, p.Lines...)
	if p.Truncated {
		lines = append(lines, m.theme.Muted.Render("…（内容已截断，"+p.Info+"）"))
	}
	return fitBlock(lines, height, width)
}
