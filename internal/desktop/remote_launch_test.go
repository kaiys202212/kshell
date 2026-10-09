package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
	"github.com/yangk/kshell/internal/terminal"
)

func newRemoteLaunchEnv(t *testing.T, run remotefs.Runner, ps []providers.Provider) (*App, *stubTermBackend, *stubLauncher, string) {
	t.Helper()
	root := t.TempDir()
	store := remote.NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	if _, err := store.Add(remote.Connection{
		ID: "c1", Name: "测试机", Host: "10.0.0.8", User: "root", Port: 22,
	}); err != nil {
		t.Fatalf("添加连接: %v", err)
	}
	projects := discovery.NewProjectStore(filepath.Join(t.TempDir(), "projects.yaml"))
	if err := projects.AddEntry(discovery.ProjectEntry{
		Kind: discovery.KindSSH, ConnID: "c1", Path: "/home/u/proj",
	}); err != nil {
		t.Fatalf("登记 ssh 项目: %v", err)
	}

	backend := &stubTermBackend{}
	launcher := &stubLauncher{}
	res := &discovery.Result{Sessions: nil, Workspaces: nil}
	app := NewAppWith(Options{
		Home:      root,
		CachePath: filepath.Join(root, ".kshell", "cache", "index.json"),
		Providers: ps,
		Store:     store,
		Projects:  projects,
		RemoteRun: run,
		FindSSH:   func() (string, error) { return "ssh-bin", nil },
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return res, nil
		},
		Windows:   NewWindowManager(launcher, nil),
		Terminals: newTerminalManagerWith(func(string, ...any) {}, backend),
	})
	app.runScan() // applyProjects → ssh 工作区进 result
	setTools(t, app, []discovery.Tool{
		{ID: "cursor", Name: "Cursor", BinPath: "cursor-agent", Installed: true},
		{ID: "claude", Name: "Claude Code", BinPath: "claude", Installed: true},
	})
	ref := discovery.FormatSSHRef("c1", "/home/u/proj")
	return app, backend, launcher, ref
}

func TestWorkspaceByIDFindsSSHRef(t *testing.T) {
	app, _, _, ref := newRemoteLaunchEnv(t, nil, []providers.Provider{providers.Claude{}})
	ws, _, ok := app.workspaceByID(ref)
	if !ok {
		t.Fatal("应按 ssh Ref 找到工作区")
	}
	if ws.Path != ref || ws.Kind != discovery.KindSSH || ws.RemotePath != "/home/u/proj" {
		t.Fatalf("ws=%+v", ws)
	}
}

func TestRemoteNewSessionCursorUsesFolderURI(t *testing.T) {
	app, _, launch, ref := newRemoteLaunchEnv(t, nil, []providers.Provider{providers.Cursor{}, providers.Claude{}})

	if err := app.NewSessionWithTool(ref, "cursor"); err != nil {
		t.Fatalf("NewSessionWithTool: %v", err)
	}
	if len(launch.launches) == 0 {
		t.Fatal("应弹窗启动")
	}
	// LaunchSession(dir, title, psStatement) — 检查 ps 语句含 folder-uri
	stmt := strings.Join(launch.launches[len(launch.launches)-1].args, " ")
	if !strings.Contains(stmt, "--folder-uri") {
		t.Fatalf("Cursor 协议启动应含 --folder-uri, got %q", stmt)
	}
	if !strings.Contains(stmt, "vscode-remote://ssh-remote+root@10.0.0.8/home/u/proj") {
		t.Fatalf("URI 拼装不符: %q", stmt)
	}
}

func TestRemoteNewSessionClaudeUsesSSH(t *testing.T) {
	run := remotefs.Runner(func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "command -v") && strings.Contains(cmd, "claude") {
			return []byte("/usr/bin/claude\n"), nil, nil
		}
		return nil, nil, errors.New("unexpected: " + cmd)
	})
	app, backend, _, ref := newRemoteLaunchEnv(t, run, []providers.Provider{providers.Claude{}})

	info, err := app.OpenWorkspaceTerminal(ref, "claude", 80, 24)
	if err != nil {
		t.Fatalf("OpenWorkspaceTerminal: %v", err)
	}
	if info.Kind != terminal.KindNew || info.Workspace != ref {
		t.Fatalf("info=%+v", info)
	}
	spec := backend.lastSpec()
	if spec.Path != "ssh-bin" {
		t.Fatalf("Path=%q, want ssh-bin", spec.Path)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "-t") {
		t.Fatalf("交互 CLI 需要 -t: %v", spec.Args)
	}
	if !strings.Contains(joined, "cd '/home/u/proj'") || !strings.Contains(joined, "'/usr/bin/claude'") {
		t.Fatalf("远端命令不符: %v", spec.Args)
	}
}

func TestRemoteNewSessionToolNotFound(t *testing.T) {
	run := remotefs.Runner(func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "command -v") {
			return nil, nil, errors.New("exit 1")
		}
		return nil, nil, errors.New("unexpected: " + cmd)
	})
	app, _, _, ref := newRemoteLaunchEnv(t, run, []providers.Provider{providers.Claude{}})
	// 去掉本机 bin，迫使走 ssh 探测
	setTools(t, app, []discovery.Tool{
		{ID: "claude", Name: "Claude Code", BinPath: "", Installed: false},
	})

	err := app.NewSessionWithTool(ref, "claude")
	if err == nil || !strings.Contains(err.Error(), "err.remote.tool_not_found|claude") {
		t.Fatalf("期望 tool_not_found, got %v", err)
	}
}

func TestRemoteResumeSessionViaSSH(t *testing.T) {
	run := remotefs.Runner(func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "command -v") {
			return []byte("/usr/bin/claude\n"), nil, nil
		}
		return nil, nil, errors.New("unexpected: " + cmd)
	})
	app, backend, _, ref := newRemoteLaunchEnv(t, run, []providers.Provider{providers.Claude{}})
	app.mu.Lock()
	app.remoteSessions.byRef = map[string][]providers.Session{
		ref: {{ID: "remote-1", ToolID: "claude", Workspace: ref, Title: "远端会话"}},
	}
	app.mu.Unlock()

	info, err := app.OpenSessionTerminal("remote-1", 80, 24)
	if err != nil {
		t.Fatalf("OpenSessionTerminal: %v", err)
	}
	if info.SessionID != "remote-1" {
		t.Fatalf("info=%+v", info)
	}
	joined := strings.Join(backend.lastSpec().Args, " ")
	if !strings.Contains(joined, "--resume") || !strings.Contains(joined, "remote-1") {
		t.Fatalf("恢复参数不符: %v", backend.lastSpec().Args)
	}
}

func TestOpenShellTerminalSSHCdsToPath(t *testing.T) {
	app, backend, _, ref := newRemoteLaunchEnv(t, nil, []providers.Provider{providers.Claude{}})

	info, err := app.OpenShellTerminal(ref, 80, 24)
	if err != nil {
		t.Fatalf("OpenShellTerminal: %v", err)
	}
	if info.Kind != terminal.KindSSH {
		t.Fatalf("Kind=%q, want %q", info.Kind, terminal.KindSSH)
	}
	spec := backend.lastSpec()
	if spec.Path != "ssh-bin" {
		t.Fatalf("Path=%q", spec.Path)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "cd '/home/u/proj'") {
		t.Fatalf("应 cd 到远端路径: %v", spec.Args)
	}
}
