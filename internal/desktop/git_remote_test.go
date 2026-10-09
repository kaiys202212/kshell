package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/remote"
)

func TestRemoteGitStatusSuccess(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, "command -v git"):
			return []byte("/usr/bin/git"), nil, nil
		case strings.Contains(cmd, "rev-parse") && strings.Contains(cmd, "show-toplevel"):
			return []byte("/home/u/proj\n"), nil, nil
		case strings.Contains(cmd, "status") && strings.Contains(cmd, "porcelain"):
			return []byte(" M file.go\x00"), nil, nil
		case strings.Contains(cmd, "rev-parse") && strings.Contains(cmd, "abbrev-ref"):
			return []byte("main\n"), nil, nil
		case strings.Contains(cmd, "find ") && strings.Contains(cmd, ".git"):
			return []byte(""), nil, nil
		default:
			return nil, nil, fmtUnexpected(cmd)
		}
	}
	app, ref := newSSHFilesEnv(t, run)

	res, err := app.GitStatus(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsRepo || res.Branch != "main" {
		t.Fatalf("got %+v", res)
	}
	if res.Status["file.go"] != "modified" {
		t.Fatalf("status=%v", res.Status)
	}
}

func TestRemoteGitUnavailable(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "command -v git") {
			return nil, []byte("not found"), errors.New("err.ssh.exit|1")
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)

	_, err := app.GitStatus(ref)
	if err == nil || !strings.Contains(err.Error(), "err.remote.git_unavailable") {
		t.Fatalf("期望 err.remote.git_unavailable, got %v", err)
	}
	if !errors.Is(err, errRemoteGitUnavailable) {
		t.Fatalf("errors.Is: %v", err)
	}

	_, err = app.GitSCM(ref, "", "")
	if err == nil || !errors.Is(err, errRemoteGitUnavailable) {
		t.Fatalf("GitSCM 降级: %v", err)
	}
}

func TestRemoteGitStageCommit(t *testing.T) {
	var seen []string
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		seen = append(seen, cmd)
		switch {
		case strings.Contains(cmd, "command -v git"):
			return []byte("/usr/bin/git"), nil, nil
		case strings.Contains(cmd, "git -C") && strings.Contains(cmd, "'add'"):
			return nil, nil, nil
		case strings.Contains(cmd, "git -C") && strings.Contains(cmd, "'commit'"):
			return nil, nil, nil
		default:
			return nil, nil, fmtUnexpected(cmd)
		}
	}
	app, ref := newSSHFilesEnv(t, run)

	if err := app.GitStage(ref, "", []string{"file.go"}); err != nil {
		t.Fatal(err)
	}
	if err := app.GitCommit(ref, "", "fix msg"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(seen, "\n")
	if !strings.Contains(joined, "add") || !strings.Contains(joined, "commit") {
		t.Fatalf("未看到 stage/commit: %v", seen)
	}
}
