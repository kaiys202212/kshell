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

func TestCodexTitleFromUserMessageShape(t *testing.T) {
	// 本机实测的主要形状：payload.type == "user_message"，首条常是包装文本需跳过
	dir := t.TempDir()
	path := filepath.Join(dir, "user-message.jsonl")
	content := `{"timestamp":"2026-09-09T09:55:35.660Z","type":"session_meta","payload":{"session_id":"cx-1","cwd":"d:\\ws"}}` + "\n" +
		`{"timestamp":"2026-09-09T09:55:35.660Z","type":"event_msg","payload":{"type":"user_message","message":"<manually_attached_skills>\nthese are skills"}}` + "\n" +
		`{"timestamp":"2026-09-09T09:55:35.660Z","type":"event_msg","payload":{"type":"user_message","message":"继续确认下一个决策点"}}` + "\n"
	writeFile(t, path, content)

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Codex{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "继续确认下一个决策点" {
		t.Fatalf("title = %q, want real user message", got.Title)
	}
}

func TestCodexTitleFromItemShape(t *testing.T) {
	// 旧导入会话形状：payload.item.type == "UserMessage"
	dir := t.TempDir()
	path := filepath.Join(dir, "item.jsonl")
	content := `{"timestamp":"2026-09-08T09:58:13.982Z","type":"session_meta","payload":{"session_id":"cx-2","cwd":"d:\\ws"}}` + "\n" +
		`{"timestamp":"2026-09-08T09:58:13.982Z","ordinal":2,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"item-1","content":[{"type":"text","text":"修复会话标题识别"}]}}}` + "\n"
	writeFile(t, path, content)

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Codex{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "修复会话标题识别" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestCodexTitleFallsBackToAgentMessage(t *testing.T) {
	// agent 自主执行的导入会话：用户消息全是 <external_links> 等包装文本
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-driven.jsonl")
	content := `{"timestamp":"2026-09-08T09:58:13.982Z","type":"session_meta","payload":{"session_id":"cx-3","cwd":"d:\\ws"}}` + "\n" +
		`{"timestamp":"2026-09-08T09:58:13.982Z","type":"event_msg","payload":{"type":"user_message","message":"<external_links>web results"}}` + "\n" +
		`{"timestamp":"2026-09-08T09:58:14.000Z","type":"event_msg","payload":{"type":"agent_message","message":"先读 Task 10 简报和相关代码"}}` + "\n"
	writeFile(t, path, content)

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Codex{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "先读 Task 10 简报和相关代码" {
		t.Fatalf("title = %q, want first agent message", got.Title)
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
