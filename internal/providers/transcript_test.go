package providers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/applang"
)

func TestFormatSessionMarkdownClaudeJSONL(t *testing.T) {
	// role 标题经 applang 直显：固定 en，避免全局语言态串扰。
	applang.SetForTest(t, "en")
	s := Session{
		ID:     "42a6304b-1fd3-45aa-b620-10aa37988f2a",
		ToolID: "claude",
		Title:  "修复上传白名单",
		Path:   "testdata/claude/basic.jsonl",
	}
	md, truncated, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if truncated {
		t.Fatal("小 fixture 不应截断")
	}
	if !strings.Contains(md, "## User") || !strings.Contains(md, "## Assistant") {
		t.Fatalf("应含 User/Assistant 标题, got:\n%s", md)
	}
	if !strings.Contains(md, "修复上传白名单校验") {
		t.Fatalf("应含用户原文, got:\n%s", md)
	}
	if !strings.Contains(md, "FileStorageService") {
		t.Fatalf("应含助手原文, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownGeminiJSON(t *testing.T) {
	s := Session{ID: "gemini-7f3c91", ToolID: "gemini", Path: "testdata/gemini/session.json"}
	md, _, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "帮我看下这个构建脚本") || !strings.Contains(md, "好的") {
		t.Fatalf("gemini messages 未抽出, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownOpencodeDB(t *testing.T) {
	// 正文经 applang 直显：固定 en，避免全局语言态串扰。
	applang.SetForTest(t, "en")
	s := Session{ID: "oc1", ToolID: "opencode", Title: "重构登录页", Path: `D:\fake\opencode.db`}
	md, _, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("sqlite 应返回说明文案而非读库错误: %v", err)
	}
	if !strings.Contains(md, "重构登录页") || !strings.Contains(md, "stored in a database") {
		t.Fatalf("应说明无法展开 sqlite 会话, got:\n%s", md)
	}
}

// 空 Path 是绑定错误（前端 toast），对外为 wire key。
func TestFormatSessionMarkdownNoPath(t *testing.T) {
	_, _, err := FormatSessionMarkdown(Session{ID: "x", ToolID: "claude"})
	if !errors.Is(err, errTranscriptNoPath) {
		t.Fatalf("空路径应报 errTranscriptNoPath: %v", err)
	}
	if err.Error() != "err.transcript.no_path" {
		t.Fatalf("空路径错误应为 wire key, got %q", err.Error())
	}
}

// 无法解析出轮次时走 fallback 正文（applang 直显，固定 en 断言）。
func TestFormatSessionMarkdownFallbackEmpty(t *testing.T) {
	applang.SetForTest(t, "en")
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"progress"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	md, _, err := FormatSessionMarkdown(Session{ID: "x", ToolID: "claude", Path: path})
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "No conversation content could be parsed") {
		t.Fatalf("fallback 正文未本地化, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownMissingFile(t *testing.T) {
	s := Session{ID: "x", ToolID: "claude", Path: "testdata/claude/not-exists.jsonl"}
	if _, _, err := FormatSessionMarkdown(s); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
