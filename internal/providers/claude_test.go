package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestClaudeParseSessionExtractsMetadata(t *testing.T) {
	p := Claude{}
	path := filepath.Join("testdata", "claude", "basic.jsonl")

	got, err := p.ParseSession(path, fixture(t, "basic.jsonl"))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "42a6304b-1fd3-45aa-b620-10aa37988f2a" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.ToolID != "claude" {
		t.Fatalf("tool = %q", got.ToolID)
	}
	if got.Title != "修复上传白名单校验，把 svg 从允许列表里去掉" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Messages != 3 {
		t.Fatalf("messages = %d, want 3", got.Messages)
	}
	if !got.CreatedAt.Before(got.UpdatedAt) {
		t.Fatalf("created %v should be before updated %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestClaudeParseSessionSkipsMalformedLines(t *testing.T) {
	p := Claude{}
	path := filepath.Join("testdata", "claude", "malformed.jsonl")

	got, err := p.ParseSession(path, fixture(t, "malformed.jsonl"))
	if err != nil {
		t.Fatalf("bad lines must be skipped, got error: %v", err)
	}
	if got.Title != "这条记录之前有一行坏数据" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
}

func TestClaudeResumeCmd(t *testing.T) {
	launch := Claude{}.ResumeCmd(Session{ID: "abc-123", Workspace: `D:\data\workspace\demo`}, "claude")
	if launch.Path != "claude" {
		t.Fatalf("path = %q", launch.Path)
	}
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "abc-123" {
		t.Fatalf("args = %v, want [--resume abc-123]", launch.Args)
	}
	if launch.Dir != `D:\data\workspace\demo` {
		t.Fatalf("dir = %q, want the session workspace", launch.Dir)
	}
}

func TestClaudeNewSessionCmdInjectsContext(t *testing.T) {
	launch := Claude{}.NewSessionCmd(`D:\data\workspace\demo`, "claude", []string{"a.go", "b.md"})
	if launch.Dir != `D:\data\workspace\demo` {
		t.Fatalf("dir = %q", launch.Dir)
	}
	if len(launch.Args) != 1 {
		t.Fatalf("args = %v, want a single prompt argument", launch.Args)
	}
	for _, want := range []string{"a.go", "b.md"} {
		if !strings.Contains(launch.Args[0], want) {
			t.Fatalf("prompt missing %q: %q", want, launch.Args[0])
		}
	}
}

func TestClaudeNewSessionCmdWithoutContext(t *testing.T) {
	launch := Claude{}.NewSessionCmd(`D:\data\workspace\demo`, "claude", nil)
	if len(launch.Args) != 0 {
		t.Fatalf("args = %v, want none when the context basket is empty", launch.Args)
	}
}

func TestClaudeSlugRoundTrip(t *testing.T) {
	for _, ws := range []string{`D:\data\workspace`, `D:\data\workspace\moxi\recorder`} {
		slug := (Claude{}).WorkspaceToSlug(ws)
		if got := (Claude{}).SlugToWorkspace(slug); got != ws {
			t.Fatalf("round trip mismatch: %q -> %q -> %q", ws, slug, got)
		}
	}
}

func TestClaudeDetectSpecAndRoots(t *testing.T) {
	home := t.TempDir()
	spec := Claude{}.DetectSpec(home)
	if spec.BinName != "claude" {
		t.Fatalf("bin = %q", spec.BinName)
	}
	if len(spec.ConfigDirs) == 0 {
		t.Fatal("expected config dirs for fallback detection")
	}

	roots := Claude{}.SessionRoots(home)
	want := filepath.Join(home, ".claude", "projects")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %v, want [%s]", roots, want)
	}
}
