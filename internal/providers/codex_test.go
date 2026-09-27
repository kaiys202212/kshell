package providers

import (
	"path/filepath"
	"testing"
)

func codexFixture(name string) string {
	return filepath.Join("testdata", "codex", name)
}

func TestCodexParseSessionMeta(t *testing.T) {
	path := codexFixture("session_meta.jsonl")
	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatalf("ReadHead: %v", err)
	}

	got, err := (Codex{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "01a08074-31e5-70c3-9b0b-0537b7080c30" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `d:\data\workspace\moxi\strategy` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "看一下部署脚本为什么超时" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.ToolID != "codex" {
		t.Fatalf("tool = %q", got.ToolID)
	}
	if got.Messages != 3 {
		t.Fatalf("messages = %d, want 3", got.Messages)
	}
	if got.UpdatedAt.IsZero() || got.CreatedAt.After(got.UpdatedAt) {
		t.Fatalf("timestamps look wrong: %v ~ %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestCodexParseSessionWithoutMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-meta.jsonl")
	writeFile(t, path, `{"timestamp":"2026-09-08T09:58:13.982Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n")

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Codex{}).ParseSession(path, head); err == nil {
		t.Fatal("session_meta missing should be an error")
	}
}

func TestCodexResumeCmd(t *testing.T) {
	launch := (Codex{}).ResumeCmd(Session{ID: "01a0", Workspace: `d:\data\workspace`}, "codex")
	if len(launch.Args) != 2 || launch.Args[0] != "resume" || launch.Args[1] != "01a0" {
		t.Fatalf("args = %v, want [resume 01a0]", launch.Args)
	}
	if launch.Dir != `d:\data\workspace` {
		t.Fatalf("dir = %q", launch.Dir)
	}
}

func TestCodexNewSessionCmdInjectsContext(t *testing.T) {
	launch := (Codex{}).NewSessionCmd(`d:\data\workspace`, "codex", []string{"main.go"})
	if len(launch.Args) != 1 {
		t.Fatalf("args = %v, want one prompt", launch.Args)
	}
	if !containsString(launch.Args[0], "main.go") {
		t.Fatalf("prompt missing main.go: %q", launch.Args[0])
	}
}

func TestCodexSessionRoots(t *testing.T) {
	home := t.TempDir()
	roots := (Codex{}).SessionRoots(home)
	want := filepath.Join(home, ".codex", "sessions")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %v, want [%s]", roots, want)
	}
}
