package desktop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
)

func TestScanRemoteSessionsBinding(t *testing.T) {
	const remoteHome = "/home/u"
	const remoteWS = "/home/u/proj"
	filePath := "/home/u/.claude/projects/slug/s1.jsonl"
	head := fmt.Sprintf(
		`{"type":"user","cwd":"%s","sessionId":"remote-1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`+"\n",
		remoteWS,
	)

	run := remotefs.Runner(func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, `echo -n "$HOME"`):
			return []byte(remoteHome), nil, nil
		case strings.Contains(cmd, "find") && strings.Contains(cmd, ".claude"):
			return []byte("20\t1700000000.0\t" + filePath + "\n"), nil, nil
		case strings.Contains(cmd, "head -c"):
			return []byte(head), nil, nil
		default:
			return nil, nil, fmt.Errorf("unexpected: %s", cmd)
		}
	})

	env := newFilesEnv(t)
	env.app.opts.RemoteRun = run
	env.app.opts.Providers = []providers.Provider{providers.Claude{}}
	ref := discovery.FormatSSHRef("c1", remoteWS)

	got, err := env.app.ScanRemoteSessions(ref)
	if err != nil {
		t.Fatalf("ScanRemoteSessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "remote-1" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Workspace != ref {
		t.Fatalf("Workspace 应改写为 Ref, got %q", got[0].Workspace)
	}

	cached := env.app.GetRemoteSessions(ref)
	if len(cached) != 1 || cached[0].ID != "remote-1" {
		t.Fatalf("GetRemoteSessions=%+v", cached)
	}
}

func TestScanRemoteSessionsRejectsLocal(t *testing.T) {
	env := newFilesEnv(t)
	_, err := env.app.ScanRemoteSessions(`D:\local\proj`)
	if err == nil || !strings.Contains(err.Error(), "err.remote.sessions_not_ssh") {
		t.Fatalf("期望 err.remote.sessions_not_ssh, got %v", err)
	}
}

func TestGetRemoteSessionsEmptyBeforeScan(t *testing.T) {
	env := newFilesEnv(t)
	ref := discovery.FormatSSHRef("c1", "/home/u/proj")
	if got := env.app.GetRemoteSessions(ref); got == nil || len(got) != 0 {
		t.Fatalf("未扫描应为空切片, got %+v", got)
	}
}
