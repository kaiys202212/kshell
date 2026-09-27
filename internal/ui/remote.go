package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/remote/scanners"
)

type candidatesMsg struct {
	cands []remote.Candidate
	err   error
}

type execDoneMsg struct {
	command string
	res     remote.Result
	err     error
}

type connsMsg struct {
	imported int
	err      error
}

// ensureConns 载入当前工作区可见的连接。
func (m *Model) ensureConns() {
	if m.store == nil {
		return
	}
	ws, ok := m.selectedWorkspace()
	if !ok {
		m.conns = nil
		return
	}
	m.conns = m.store.List(ws.Path)
	if m.connCursor >= len(m.conns) && len(m.conns) > 0 {
		m.connCursor = len(m.conns) - 1
	}
}

func (m Model) visibleConns() []remote.Connection {
	return m.conns
}

func (m Model) selectedConn() (remote.Connection, bool) {
	if len(m.conns) == 0 || m.connCursor >= len(m.conns) {
		return remote.Connection{}, false
	}
	return m.conns[m.connCursor], true
}

func (m Model) sshOptions() remote.SSHOptions {
	c := m.opts.Config.SSHOptions
	return remote.SSHOptions{
		ConnectTimeout:        c.ConnectTimeout,
		ExtraArgs:             c.ExtraArgs,
		CommandTimeoutSeconds: c.CommandTimeoutSeconds,
	}
}

func (m Model) renderRemoteBody(height, width int) string {
	if m.importing {
		return m.renderCandidates(height, width)
	}

	lines := []string{m.sectionHeader("CONNECTIONS", "")}
	for i, c := range m.visibleConns() {
		if len(lines) >= height-1 {
			break
		}
		lines = append(lines, m.renderRow(i == m.connCursor, connLabel(c)))
	}
	if len(m.conns) == 0 {
		lines = append(lines, m.theme.Muted.Render("（没有连接，按 i 扫描导入）"))
	}
	return fitBlock(lines, height, width)
}

func (m Model) renderCandidates(height, width int) string {
	lines := []string{m.sectionHeader("CANDIDATES（空格勾选，回车导入）", "")}
	for i, c := range m.candidates {
		if len(lines) >= height-1 {
			break
		}
		mark := "[ ]"
		if m.candChecked[candKey(c)] {
			mark = "[x]"
		}
		lines = append(lines, m.renderRow(i == m.candCursor,
			mark+" "+c.Target()+"  "+c.Confidence+"  "+c.Source))
	}
	if len(m.candidates) == 0 {
		lines = append(lines, m.theme.Muted.Render("（没有扫到候选连接）"))
	}
	return fitBlock(lines, height, width)
}

func (m Model) renderRemoteRight(height, width int) string {
	if m.importing {
		lines := []string{m.sectionHeader("CANDIDATE", "")}
		if len(m.candidates) == 0 || m.candCursor >= len(m.candidates) {
			lines = append(lines, "", m.theme.Muted.Render("无候选项"))
			return fitBlock(lines, height, width)
		}
		c := m.candidates[m.candCursor]
		lines = append(lines,
			m.theme.Body.Render(c.Target()),
			"",
			m.theme.Muted.Render("来源   "+c.Source),
			m.theme.Muted.Render("文件   "+c.SourceFile),
			m.theme.Muted.Render("行号   "+itoa(c.SourceLine)),
			m.theme.Muted.Render("密钥   "+orDefault(c.IdentityFile, "-")),
		)
		return fitBlock(lines, height, width)
	}

	lines := []string{m.sectionHeader("OUTPUT", "")}
	lines = append(lines, m.output...)
	if m.execRunning {
		lines = append(lines, m.theme.Muted.Render("执行中…"))
	}
	return fitBlock(lines, height, width)
}

func (m Model) renderRemoteStatusLine() string {
	c, ok := m.selectedConn()
	if !ok {
		return "i 扫描导入  x 执行命令  s 交互式  t 测试  b 绑定  d 删除"
	}
	return "目标 " + c.Display() + "  i 导入  x 执行  s 交互式  t 测试  b 绑定  d 删除"
}

// scanCandidatesCmd 扫描工作区（外加 ~/.ssh/config）收集候选连接。
func (m Model) scanCandidatesCmd() tea.Cmd {
	return func() tea.Msg {
		ws, ok := m.selectedWorkspace()
		if !ok {
			return candidatesMsg{err: errors.New("没有选中的工作区")}
		}

		cands, err := remote.ScanWorkspace(ws.Path, m.opts.Scanners, m.opts.Config.Scanners, m.opts.Config.Exclude)
		if err != nil {
			return candidatesMsg{cands: cands, err: err}
		}

		if enabled, ok := m.opts.Config.Scanners["sshconfig"]; !ok || enabled {
			cands = append(cands, globalSSHConfigCandidates()...)
		}
		return candidatesMsg{cands: remote.Dedupe(cands)}
	}
}

func globalSSHConfigCandidates() []remote.Candidate {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".ssh", "config")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	cands := scanners.ParseSSHConfig(f)
	for i := range cands {
		cands[i].SourceFile = path
	}
	return cands
}

func (m *Model) toggleCandidate() {
	if m.candCursor >= len(m.candidates) {
		return
	}
	c := m.candidates[m.candCursor]
	if m.candChecked == nil {
		m.candChecked = map[string]bool{}
	}
	m.candChecked[candKey(c)] = !m.candChecked[candKey(c)]
}

// importCheckedCmd 只写入被勾选的候选；低置信度默认不勾，避免脏数据入库。
func (m Model) importCheckedCmd() tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return connsMsg{err: errors.New("连接存储未初始化")}
		}
		ws, ok := m.selectedWorkspace()
		if !ok {
			return connsMsg{err: errors.New("没有选中的工作区")}
		}

		imported := 0
		for _, c := range m.candidates {
			if !m.candChecked[candKey(c)] {
				continue
			}
			_, err := m.store.Add(remote.Connection{
				Name:         c.Name,
				Host:         c.Host,
				User:         c.User,
				Port:         c.Port,
				IdentityFile: c.IdentityFile,
				Workspace:    ws.Path,
				Source:       c.Source,
				SourceFile:   c.SourceFile,
			})
			if err == nil {
				imported++
			}
		}
		return connsMsg{imported: imported}
	}
}

func (m Model) execRemoteCmd(command string) tea.Cmd {
	return func() tea.Msg {
		c, ok := m.selectedConn()
		if !ok {
			return execDoneMsg{err: errors.New("没有选中的连接")}
		}
		res, err := remote.Run(context.Background(), c, command, m.sshOptions())
		return execDoneMsg{command: command, res: res, err: err}
	}
}

func (m Model) testConnCmd() tea.Cmd {
	return m.execRemoteCmd("true")
}

func (m Model) shellCmd() tea.Cmd {
	bin, err := remote.FindSSH()
	if err != nil {
		return statusCmd(err.Error(), true)
	}
	c, ok := m.selectedConn()
	if !ok {
		return statusCmd("没有选中的连接", true)
	}
	spec := launcher.Spec{Path: bin, Args: remote.ShellArgs(c, m.sshOptions())}
	return tea.ExecProcess(spec.Cmd(context.Background()), func(err error) tea.Msg {
		if err != nil {
			return statusMsg{text: "ssh 退出异常：" + err.Error(), warn: true}
		}
		return statusMsg{text: "已断开 " + c.Display()}
	})
}

func (m *Model) deleteSelectedConn() {
	if m.store == nil {
		return
	}
	c, ok := m.selectedConn()
	if !ok {
		return
	}
	if err := m.store.Delete(c.ID); err != nil {
		m.status, m.statusWarn = "删除失败："+err.Error(), true
		return
	}
	m.status, m.statusWarn = "已删除 "+c.Display(), false
	m.ensureConns()
}

func (m *Model) bindSelectedConn() {
	if m.store == nil {
		return
	}
	c, ok := m.selectedConn()
	if !ok {
		return
	}
	ws, ok := m.selectedWorkspace()
	if !ok {
		return
	}
	c.Workspace = ws.Path
	if err := m.store.Update(c); err != nil {
		m.status, m.statusWarn = "绑定失败："+err.Error(), true
		return
	}
	m.status, m.statusWarn = "已绑定到当前工作区", false
	m.ensureConns()
}

func connLabel(c remote.Connection) string {
	label := c.Name + "  " + c.Display()
	if c.Verified {
		label += "  ✓"
	}
	if c.Source != "" {
		label += "  (" + c.Source + ")"
	}
	return label
}

func candKey(c remote.Candidate) string {
	return strings.ToLower(c.Host) + "|" + strings.ToLower(c.User) + "|" + strconv.Itoa(c.Port)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
