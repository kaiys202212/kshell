package providers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cursorPath 构造形如 ~/.cursor/projects/<slug>/agent-transcripts/<uuid>/<uuid>.jsonl 的路径。
func cursorPath(slug, uuid string) string {
	return filepath.Join("testdata", "cursor", "projects", slug, "agent-transcripts", uuid, uuid+".jsonl")
}

func cursorFixture(t *testing.T, slug, uuid string) []byte {
	t.Helper()
	data, err := os.ReadFile(cursorPath(slug, uuid))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestCursorParseSessionExtractsMetadata(t *testing.T) {
	p := Cursor{}
	const uuid = "3f2a1111-2222-4333-8444-555566667777"
	path := cursorPath("d-data-workspace-moxi-kshell", uuid)

	got, err := p.ParseSession(path, cursorFixture(t, "d-data-workspace-moxi-kshell", uuid))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != uuid {
		t.Fatalf("id = %q, want uuid from path", got.ID)
	}
	if got.ToolID != "cursor" {
		t.Fatalf("tool = %q", got.ToolID)
	}
	if got.Title != "修复登录页的空指针异常" {
		t.Fatalf("title = %q, want <user_query> 内容", got.Title)
	}
	if got.Workspace != `d:\data\workspace\moxi\kshell` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Messages != 4 {
		t.Fatalf("messages = %d, want 4", got.Messages)
	}
	// 记录里没有时间戳，必须回退到文件 mtime
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated = zero, want file mtime")
	}
	if !got.CreatedAt.Equal(got.UpdatedAt) {
		t.Fatalf("created %v != updated %v（无时间戳时两者同源）", got.CreatedAt, got.UpdatedAt)
	}
}

func TestCursorParseSessionRejectsEmpty(t *testing.T) {
	p := Cursor{}
	const uuid = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0000"
	path := cursorPath("d-data-empty", uuid)

	if _, err := p.ParseSession(path, cursorFixture(t, "d-data-empty", uuid)); !errors.Is(err, errCursorEmpty) {
		t.Fatalf("err = %v, want errCursorEmpty", err)
	}
}

func TestCursorMatchSessionRel(t *testing.T) {
	p := Cursor{}
	cases := []struct {
		rel  string
		want bool
	}{
		{"d-data-ws/agent-transcripts/uuid-1/uuid-1.jsonl", true},
		{"d-data-ws/agent-transcripts/uuid-1/subagents/agent-1.jsonl", false},
		{"d-data-ws/terminals/uuid-1/uuid-1.jsonl", false},
		{"d-data-ws/agent-transcripts/uuid-1", false},
		{"d-data-ws/notes.md", false},
	}
	for _, c := range cases {
		if got := p.MatchSessionRel(c.rel); got != c.want {
			t.Errorf("MatchSessionRel(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

func TestCursorSlugToWorkspace(t *testing.T) {
	cases := []struct {
		slug string
		want string
	}{
		{"d-data-workspace-moxi-agent", `d:\data\workspace\moxi\agent`},
		{"c-Users-yangk-demo", `c:\Users\yangk\demo`},
		{"", ""},
	}
	for _, c := range cases {
		if got := (Cursor{}).SlugToWorkspace(c.slug); got != c.want {
			t.Errorf("SlugToWorkspace(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestCursorResumeCmd(t *testing.T) {
	p := Cursor{}
	s := Session{ID: "uuid-9", ToolID: "cursor", Workspace: `d:\data\ws`}
	got := p.ResumeCmd(s, "cursor-agent")

	if got.Path != "cursor-agent" {
		t.Fatalf("path = %q", got.Path)
	}
	if strings.Join(got.Args, " ") != "--resume uuid-9" {
		t.Fatalf("args = %v", got.Args)
	}
	if got.Dir != `d:\data\ws` {
		t.Fatalf("dir = %q", got.Dir)
	}
}
