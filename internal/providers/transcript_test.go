package providers

import (
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/applang"
)

func TestFormatSessionMarkdownClaudeJSONL(t *testing.T) {
	// role 标题经 applang 直显：固定 en，避免全局语言态串扰。
	applang.Set("en")
	t.Cleanup(func() { applang.Set("en") })
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
	s := Session{ID: "oc1", ToolID: "opencode", Title: "重构登录页", Path: `D:\fake\opencode.db`}
	md, _, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("sqlite 应返回说明文案而非读库错误: %v", err)
	}
	if !strings.Contains(md, "重构登录页") || !strings.Contains(md, "数据库") {
		t.Fatalf("应说明无法展开 sqlite 会话, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownMissingFile(t *testing.T) {
	s := Session{ID: "x", ToolID: "claude", Path: "testdata/claude/not-exists.jsonl"}
	if _, _, err := FormatSessionMarkdown(s); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
