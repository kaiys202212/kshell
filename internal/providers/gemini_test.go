package providers

import (
	"path/filepath"
	"testing"
)

func TestGeminiParseSession(t *testing.T) {
	path := filepath.Join("testdata", "gemini", "session.json")
	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatalf("ReadHead: %v", err)
	}

	got, err := (Gemini{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "gemini-7f3c91" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "帮我看下这个构建脚本" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Messages != 2 {
		t.Fatalf("messages = %d, want 2", got.Messages)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("timestamps: %v ~ %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGeminiParseSessionWithoutID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	writeFile(t, path, `{"cwd":"D:\\ws"}`)

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Gemini{}).ParseSession(path, head); err == nil {
		t.Fatal("missing sessionId should be an error")
	}
}

// TestGeminiParseSessionTruncatedHead 覆盖 2MB 截断：单 JSON 会话超长时 head
// 是残缺 JSON（截掉尾部），json.Unmarshal 会失败把好会话记成 failed。
// 用流式 Decoder 只取首个 JSON 值，截断尾部不致命——sessionId/标题在头部就能解析。
func TestGeminiParseSessionTruncatedHead(t *testing.T) {
	path := filepath.Join("testdata", "gemini", "session.json")
	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟截断：砍掉尾部 40% 字节（messages 数组必然被腰斩）
	cut := head[:len(head)*6/10]

	got, err := (Gemini{}).ParseSession(path, cut)
	if err != nil {
		t.Fatalf("截断 head 不应解析失败: %v", err)
	}
	if got.ID != "gemini-7f3c91" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
}

func TestGeminiFilePatternIsJSON(t *testing.T) {
	if got := (Gemini{}).SessionFilePattern(); got != "*.json" {
		t.Fatalf("pattern = %q, want *.json", got)
	}
	if got := (Claude{}).SessionFilePattern(); got != "*.jsonl" {
		t.Fatalf("claude pattern = %q, want *.jsonl", got)
	}
	if got := (Codex{}).SessionFilePattern(); got != "*.jsonl" {
		t.Fatalf("codex pattern = %q, want *.jsonl", got)
	}
}

func TestGeminiResumeCmd(t *testing.T) {
	launch := (Gemini{}).ResumeCmd(Session{ID: "gemini-7f3c91", Workspace: `D:\ws`}, "gemini")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "gemini-7f3c91" {
		t.Fatalf("args = %v", launch.Args)
	}
	if launch.Dir != `D:\ws` {
		t.Fatalf("dir = %q", launch.Dir)
	}
}
