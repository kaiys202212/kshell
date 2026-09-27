package ui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/remote/scanners"
)

func fakeSSH(t *testing.T, dir, body string) {
	t.Helper()
	var path string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "ssh.cmd")
		body = "@echo off\r\n" + body + "\r\n"
	} else {
		path = filepath.Join(dir, "ssh")
		body = "#!/bin/sh\n" + body + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func modelWithStore(t *testing.T) (Model, *remote.Store, string) {
	t.Helper()
	// 隔离 HOME：否则全局 ~/.ssh/config 会把真实连接混进候选列表
	fakeHome := t.TempDir()
	t.Setenv("USERPROFILE", fakeHome)
	t.Setenv("HOME", fakeHome)

	root := t.TempDir()
	sessions := []providers.Session{{ID: "s1", ToolID: "claude", Workspace: root, UpdatedAt: time.Now()}}

	store := remote.NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	m := NewModelWith(Options{
		Store:     store,
		Scanners:  []remote.Scanner{scanners.SSHConfigScanner{}, scanners.EnvScanner{}, scanners.DeployScanner{}},
		Providers: []providers.Provider{providers.Claude{}},
	})
	m.workspaces = discovery.GroupSessions(sessions)
	m.sessions = sessions
	m.view = ViewRemote
	m.ensureConns()
	return m, store, root
}

func TestRemoteViewListsOnlyWorkspaceConnections(t *testing.T) {
	m, store, root := modelWithStore(t)
	if _, err := store.Add(remote.Connection{Name: "mine", Host: "10.0.0.1", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(remote.Connection{Name: "other", Host: "10.0.0.2", Workspace: "D:\\elsewhere"}); err != nil {
		t.Fatal(err)
	}
	m.ensureConns()

	out := m.View()
	if !strings.Contains(out, "mine") {
		t.Fatalf("bound connection missing:\n%s", out)
	}
	if strings.Contains(out, "other") {
		t.Fatalf("other workspaces must not leak in:\n%s", out)
	}
}

func TestImportFlowOnlyWritesCheckedCandidates(t *testing.T) {
	m, store, root := modelWithStore(t)
	writeFileIfMissing(t, filepath.Join(root, ".env"), "SSH_HOST=10.0.0.9\nSSH_USER=ops\n")

	// 扫描
	msg := m.scanCandidatesCmd()()
	got, ok := msg.(candidatesMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if len(got.cands) == 0 {
		t.Fatal("expected at least one candidate")
	}

	next, _ := m.Update(got)
	withCands := next.(Model)
	if !withCands.importing {
		t.Fatal("scanning should switch to import mode")
	}
	if len(withCands.candChecked) != 0 {
		t.Fatal("nothing should be checked by default")
	}

	// 勾选第一个后导入
	next, _ = withCands.Update(tea.KeyMsg{Type: tea.KeySpace})
	checked := next.(Model)
	if len(checked.candChecked) != 1 {
		t.Fatalf("space should check one candidate: %v", checked.candChecked)
	}

	importMsg := checked.importCheckedCmd()()
	if got2, ok := importMsg.(connsMsg); !ok || got2.imported != 1 {
		t.Fatalf("import msg = %+v, want 1 imported", importMsg)
	}

	reloaded := remote.NewStore(store.Path())
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	conns := reloaded.List(root)
	if len(conns) != 1 {
		t.Fatalf("connections = %+v, want 1", conns)
	}
	if conns[0].Host != "10.0.0.9" || conns[0].User != "ops" {
		t.Fatalf("imported = %+v", conns[0])
	}
	if conns[0].Workspace != root {
		t.Fatalf("imported connection must be bound to the workspace: %+v", conns[0])
	}
	if conns[0].Source == "" {
		t.Fatalf("source should be recorded: %+v", conns[0])
	}
}

func TestExecuteCommandShowsOutput(t *testing.T) {
	dir := t.TempDir()
	fakeSSH(t, dir, "echo linux-kernel")
	t.Setenv("PATH", dir)

	m, store, root := modelWithStore(t)
	if _, err := store.Add(remote.Connection{Name: "mine", Host: "10.0.0.1", User: "root", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	m.ensureConns()

	msg := m.execRemoteCmd("uname -a")()
	done, ok := msg.(execDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if done.err != nil {
		t.Fatalf("exec error: %v", done.err)
	}
	if !strings.Contains(done.res.Stdout, "linux-kernel") {
		t.Fatalf("stdout = %q", done.res.Stdout)
	}

	next, _ := m.Update(done)
	got := next.(Model)
	if !strings.Contains(got.View(), "linux-kernel") {
		t.Fatalf("output should show in the right panel:\n%s", got.View())
	}
	if !strings.Contains(got.status, "退出码") {
		t.Fatalf("status should show the exit code, got %q", got.status)
	}
}

func TestExecFailureShowsError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 没有 ssh

	m, store, root := modelWithStore(t)
	if _, err := store.Add(remote.Connection{Name: "mine", Host: "h", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	m.ensureConns()

	msg := m.execRemoteCmd("ls")()
	done := msg.(execDoneMsg)
	if done.err == nil {
		t.Fatal("missing ssh must surface an error")
	}

	next, _ := m.Update(done)
	got := next.(Model)
	if !got.statusWarn {
		t.Fatalf("failure should be a warning status, got %q", got.status)
	}
}

func TestCommandInputMode(t *testing.T) {
	m, _, _ := modelWithStore(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	got := next.(Model)
	if !got.cmdInputting {
		t.Fatal("x should start command input")
	}

	for _, r := range "ls -la" {
		next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		got = next.(Model)
	}
	if got.cmdInput != "ls -la" {
		t.Fatalf("cmdInput = %q", got.cmdInput)
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(Model)
	if got.cmdInputting {
		t.Fatal("esc should cancel input")
	}
}

func TestBindAndDeleteConnection(t *testing.T) {
	m, store, root := modelWithStore(t)
	if _, err := store.Add(remote.Connection{Name: "mine", Host: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	m.ensureConns()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	got := next.(Model)
	if len(got.conns) != 1 {
		t.Fatalf("conns = %+v, want 1 after binding", got.conns)
	}
	if got.conns[0].Workspace != root {
		t.Fatalf("binding failed, workspace = %q", got.conns[0].Workspace)
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got = next.(Model)
	if len(got.conns) != 0 {
		t.Fatalf("delete should leave no connections, got %+v", got.conns)
	}
}

func TestShellCmdFailsWithoutSSH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m, store, root := modelWithStore(t)
	if _, err := store.Add(remote.Connection{Name: "mine", Host: "h", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	m.ensureConns()

	cmd := m.shellCmd()
	if cmd == nil {
		t.Fatal("shellCmd should still return a cmd (a status message)")
	}
	msg := cmd()
	if s, ok := msg.(statusMsg); !ok || !s.warn {
		t.Fatalf("expected a warning status, got %#v", msg)
	}
}

func writeFileIfMissing(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ = context.Background
