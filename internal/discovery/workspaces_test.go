package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

func TestNormalizePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := NormalizePath(`D:\data\workspace\`); got != `d:\data\workspace` {
			t.Fatalf("got %q, want %q", got, `d:\data\workspace`)
		}
		// 同一目录的不同写法必须归一到同一个 key（Windows 大小写不敏感 + 分隔符两种）
		if NormalizePath(`D:/data/workspace`) != NormalizePath(`d:\data\workspace`) {
			t.Fatal("separator and case variants should normalize equally")
		}
		return
	}

	if got := NormalizePath("/data/workspace/"); got != "/data/workspace" {
		t.Fatalf("got %q, want %q", got, "/data/workspace")
	}
}

func TestGroupSessionsIntoWorkspaces(t *testing.T) {
	now := time.Now()
	sessions := []providers.Session{
		{ID: "1", ToolID: "claude", Workspace: filepath.Join("D:", "data", "ws-a"), UpdatedAt: now.Add(-time.Hour)},
		{ID: "2", ToolID: "codex", Workspace: filepath.Join("D:", "data", "ws-b"), UpdatedAt: now},
		{ID: "3", ToolID: "claude", Workspace: filepath.Join("D:", "data", "ws-a"), UpdatedAt: now.Add(-time.Minute)},
	}

	ws := GroupSessions(sessions)
	if len(ws) != 2 {
		t.Fatalf("got %d workspaces, want 2: %+v", len(ws), ws)
	}
	if ws[0].Path != filepath.Join("D:", "data", "ws-b") {
		t.Fatalf("most recently used should come first, got %q", ws[0].Path)
	}

	var a *Workspace
	for i := range ws {
		if ws[i].Path == filepath.Join("D:", "data", "ws-a") {
			a = &ws[i]
		}
	}
	if a == nil {
		t.Fatal("ws-a missing")
	}
	if a.SessionCount != 2 {
		t.Fatalf("session count = %d, want 2", a.SessionCount)
	}
	if a.ToolCounts["claude"] != 2 {
		t.Fatalf("claude count = %d, want 2", a.ToolCounts["claude"])
	}
}

func TestGroupSessionsSkipsEmptyWorkspace(t *testing.T) {
	ws := GroupSessions([]providers.Session{{ID: "1", Workspace: ""}})
	if len(ws) != 0 {
		t.Fatalf("sessions without a workspace must be dropped, got %+v", ws)
	}
}

func TestScanGitReposRespectsDepthAndExcludes(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "repo-a", ".git"))
	mkdirAll(t, filepath.Join(root, "node_modules", "dep", ".git"))
	mkdirAll(t, filepath.Join(root, "deep", "l1", "l2", "l3", ".git"))

	got := ScanGitRepos([]string{root}, 2, []string{".git", "node_modules", "vendor", "dist", "build"})

	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1: %+v", len(got), got)
	}
	want := filepath.Join(root, "repo-a")
	if NormalizePath(got[0].Path) != NormalizePath(want) {
		t.Fatalf("got %q, want %q", got[0].Path, want)
	}
	if got[0].Source != "git" {
		t.Fatalf("source = %q, want git", got[0].Source)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
