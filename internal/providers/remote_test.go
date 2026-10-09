package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestSSHRemoteFolderURI(t *testing.T) {
	got := SSHRemoteFolderURI("alice@dev.example.com", "/home/alice/proj")
	want := "vscode-remote://ssh-remote+alice@dev.example.com/home/alice/proj"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// 路径无 leading slash 时补上
	got = SSHRemoteFolderURI("host", "var/www")
	if got != "vscode-remote://ssh-remote+host/var/www" {
		t.Fatalf("got %q", got)
	}
}

func TestCursorRemoteLauncherUsesFolderURI(t *testing.T) {
	p := Cursor{}
	l, err := p.NewRemoteSessionCmd("u@h", "/home/u/ws", "cursor-agent")
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "cursor-agent" {
		t.Fatalf("Path = %q", l.Path)
	}
	if len(l.Args) != 2 || l.Args[0] != "--folder-uri" {
		t.Fatalf("Args = %v", l.Args)
	}
	if l.Args[1] != "vscode-remote://ssh-remote+u@h/home/u/ws" {
		t.Fatalf("URI = %q", l.Args[1])
	}
	if l.Dir != "" {
		t.Fatalf("Dir 应为空（远程协议启动不依赖本机 cwd），got %q", l.Dir)
	}

	resume, err := p.ResumeRemoteCmd("u@h", "/home/u/ws", Session{ID: "chat-1"}, "cursor-agent")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(resume.Args, " ")
	if !strings.Contains(joined, "--folder-uri") || !strings.Contains(joined, "--resume chat-1") {
		t.Fatalf("Resume Args = %v", resume.Args)
	}
}

func TestRemoteSSHRunnerArgs(t *testing.T) {
	cases := []struct {
		name   string
		p      RemoteSSHRunner
		resume []string
	}{
		{"claude", Claude{}, []string{"--resume", "s1"}},
		{"codex", Codex{}, []string{"resume", "s1"}},
		{"gemini", Gemini{}, []string{"--resume", "s1"}},
		{"opencode", Opencode{}, []string{"--session", "s1"}},
		{"codebuddy", CodeBuddy{}, []string{"--resume", "s1"}},
		{"cursor-fallback", Cursor{}, []string{"--resume", "s1"}},
	}
	s := Session{ID: "s1", Workspace: "/home/u/ws"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.RemoteNewArgs("/home/u/ws"); len(got) != 0 {
				t.Fatalf("RemoteNewArgs = %v, want 空（仅 cd 后起 bin）", got)
			}
			got := tc.p.RemoteResumeArgs(s)
			if strings.Join(got, " ") != strings.Join(tc.resume, " ") {
				t.Fatalf("RemoteResumeArgs = %v, want %v", got, tc.resume)
			}
		})
	}
}

func TestRemoteShellCommand(t *testing.T) {
	got := RemoteShellCommand("/home/u/ws", "/usr/bin/claude", []string{"--resume", "abc"})
	want := "cd '/home/u/ws' && '/usr/bin/claude' '--resume' 'abc'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// 单引号转义
	got = RemoteShellCommand("/tmp/a'b", "claude", nil)
	if !strings.Contains(got, `'/tmp/a'\''b'`) {
		t.Fatalf("quoting failed: %q", got)
	}
}

func TestErrRemoteToolNotFound(t *testing.T) {
	err := ErrRemoteToolNotFound("claude")
	if err.Error() != "err.remote.tool_not_found|claude" {
		t.Fatalf("got %q", err.Error())
	}
	if !errors.Is(err, errRemoteToolNotFound) {
		t.Fatal("errors.Is 应对哨兵成立")
	}
}
