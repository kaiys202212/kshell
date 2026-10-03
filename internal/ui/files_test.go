package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/workspace"
)

func modelWithFiles(t *testing.T) (Model, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions := []providers.Session{{ID: "s1", ToolID: "claude", Workspace: root, UpdatedAt: time.Now()}}
	m := NewModel()
	m.workspaces = discovery.GroupSessions(sessions)
	m.sessions = sessions
	m.tools = []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude"}}
	m.view = ViewFiles
	m.ensureTree()
	return m, root
}

func TestFilesViewListsWorkspaceEntries(t *testing.T) {
	m, _ := modelWithFiles(t)

	out := m.View()
	if !strings.Contains(out, "main.go") {
		t.Fatalf("file tree missing main.go:\n%s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("ignored dir must not show:\n%s", out)
	}
}

func TestFilesViewExpandsDirectory(t *testing.T) {
	m, root := modelWithFiles(t)

	_ = root
	rows := m.fileRows()
	idx := -1
	for i, r := range rows {
		if r.Node.Name == "sub" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("sub row missing: %v", rowNames(rows))
	}

	m.fileCursor = idx
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	out := got.View()
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("expanding sub should reveal a.txt:\n%s", out)
	}
}

func TestShowAllToggleRevealsIgnored(t *testing.T) {
	m, _ := modelWithFiles(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := next.(Model)
	if !got.showAll {
		t.Fatal("a should toggle showAll")
	}
	if !strings.Contains(got.View(), "node_modules") {
		t.Fatalf("showAll should reveal node_modules:\n%s", got.View())
	}
}

func TestFilesPreviewShowsContent(t *testing.T) {
	m, _ := modelWithFiles(t)
	idx := indexOfRow(m.fileRows(), "main.go")
	if idx < 0 {
		t.Fatal("main.go row missing")
	}
	m.fileCursor = idx

	// 预览是异步加载的：先拿到加载命令，把结果喂回模型
	load := m.ensurePreview()
	if load == nil {
		t.Fatal("expected a preview load command")
	}
	next, _ := m.Update(load())
	got := next.(Model)

	out := got.View()
	if !strings.Contains(out, "func main()") {
		t.Fatalf("preview should show file content:\n%s", out)
	}
	if strings.Contains(out, "加载中") {
		t.Fatalf("loaded preview should not show loading:\n%s", out)
	}
}

func TestFilesPreviewIsAsync(t *testing.T) {
	m, _ := modelWithFiles(t)
	m.previewPending = true

	out := m.View()
	if strings.Contains(out, "package main") {
		t.Fatal("render must not read disk synchronously; content only arrives via previewMsg")
	}
}

func TestNewSessionFromFilesViewStartsInWorkspace(t *testing.T) {
	m, root := modelWithFiles(t)

	launch, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if len(launch.Args) != 0 {
		t.Fatalf("新建会话不应带任何文件参数: %v", launch.Args)
	}
	if launch.Dir != root {
		t.Fatalf("dir = %q, want %q", launch.Dir, root)
	}
}

func indexOfRow(rows []workspace.Row, name string) int {
	for i, r := range rows {
		if r.Node.Name == name {
			return i
		}
	}
	return -1
}

func rowNames(rows []workspace.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Node.Name)
	}
	return out
}
