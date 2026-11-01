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
	md, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
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

// 会话预览必须全量读入：超过旧 512KB 上限的文件，末尾轮次不得丢失。
func TestFormatSessionMarkdownLargeFileFullyRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"role":"user","content":"开头的第一条消息"}}` + "\n")
	// 填充到远超旧 512KB 上限的噪音轮次。
	noise := strings.Repeat("占位内容。", 1024) // 约 5KB
	for i := 0; i < 120; i++ {
		b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"` + noise + `"}}` + "\n")
	}
	b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":"结尾的最后一条助手消息"}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() <= 512*1024 {
		t.Fatalf("fixture 应大于 512KB, size=%d err=%v", fi.Size(), err)
	}
	md, err := FormatSessionMarkdown(Session{ID: "big", ToolID: "claude", Path: path})
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "开头的第一条消息") {
		t.Fatalf("应含首条消息, got len=%d", len(md))
	}
	if !strings.Contains(md, "结尾的最后一条助手消息") {
		t.Fatalf("超过 512KB 的末尾消息不得被截断, got len=%d", len(md))
	}
}

func TestFormatSessionMarkdownGeminiJSON(t *testing.T) {
	s := Session{ID: "gemini-7f3c91", ToolID: "gemini", Path: "testdata/gemini/session.json"}
	md, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "帮我看下这个构建脚本") || !strings.Contains(md, "好的") {
		t.Fatalf("gemini messages 未抽出, got:\n%s", md)
	}
}

// opencodeTranscriptTurns：CLI 读 SQLite 还原 user/assistant 轮次（时间正序），
// 过滤空文本与非 user/assistant 角色，会话 ID 单引号双写转义，SQL 单行。
func TestOpencodeTranscriptTurns(t *testing.T) {
	origQ, origBin := runOpencodeQuery, opencodeDetectBin
	defer func() { runOpencodeQuery, opencodeDetectBin = origQ, origBin }()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("无 home: %v", err)
	}
	var gotSQL string
	runOpencodeQuery = func(bin, query string) ([]byte, error) {
		gotSQL = query
		return []byte(`[
			{"role":"user","text":"打开文件"},
			{"role":"assistant","text":"好的"},
			{"role":"system","text":"跳过"},
			{"role":"user","text":"   "},
			{"role":"user","text":"第二问"}
		]`), nil
	}
	opencodeDetectBin = func(string) string { return "/fake/bin" }

	s := Session{ID: "ses_o'1", ToolID: "opencode", Workspace: "/w", Path: opencodeDBPath(home)}
	turns, err := opencodeTranscriptTurns(s)
	if err != nil {
		t.Fatalf("opencodeTranscriptTurns: %v", err)
	}
	if len(turns) != 3 || turns[0].Text != "打开文件" || turns[0].Role != "user" ||
		turns[1].Text != "好的" || turns[2].Text != "第二问" {
		t.Fatalf("turns = %+v", turns)
	}
	if !strings.Contains(gotSQL, "ses_o''1") || strings.ContainsAny(gotSQL, "\r\n") {
		t.Fatalf("SQL 转义/单行不符: %q", gotSQL)
	}
}

// opencodeTranscriptTurns 的不可用口径：远端会话、库口径不符、CLI 缺失、查询失败，
// 一律返回 errOpencodeTranscriptUnavailable，且不可用路径不得发查询。
func TestOpencodeTranscriptUnavailable(t *testing.T) {
	origQ, origBin := runOpencodeQuery, opencodeDetectBin
	defer func() { runOpencodeQuery, opencodeDetectBin = origQ, origBin }()
	called := false
	runOpencodeQuery = func(_, _ string) ([]byte, error) { called = true; return nil, nil }

	// 远端会话显式拒绝（本地 CLI 查不到远端库，防止串库）
	if _, err := opencodeTranscriptTurns(Session{ID: "s", ToolID: "opencode", Workspace: "ssh://x", Path: "db"}); err == nil {
		t.Fatal("ssh 会话应不可用")
	}
	// Path 与默认库不符（如其它 home 的 db 或绑定错误）
	if _, err := opencodeTranscriptTurns(Session{ID: "s", ToolID: "opencode", Workspace: "/w", Path: `D:\fake\opencode.db`}); err == nil {
		t.Fatal("库口径不符应不可用")
	}
	if called {
		t.Fatal("不可用路径不应发查询")
	}
	// CLI 探测为空
	opencodeDetectBin = func(string) string { return "" }
	home, _ := os.UserHomeDir()
	if _, err := opencodeTranscriptTurns(Session{ID: "s", ToolID: "opencode", Workspace: "/w", Path: opencodeDBPath(home)}); err == nil {
		t.Fatal("无 bin 应不可用")
	}
	if called {
		t.Fatal("不可用路径不应发查询")
	}
	// 查询失败
	opencodeDetectBin = func(string) string { return "/fake/bin" }
	runOpencodeQuery = func(_, _ string) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := opencodeTranscriptTurns(Session{ID: "s", ToolID: "opencode", Workspace: "/w", Path: opencodeDBPath(home)}); err == nil {
		t.Fatal("查询失败应不可用")
	}
}

// FormatSessionMarkdown 接线：查询成功时渲染全文轮次。
func TestFormatSessionMarkdownOpencodeFullTranscript(t *testing.T) {
	origQ, origBin := runOpencodeQuery, opencodeDetectBin
	defer func() { runOpencodeQuery, opencodeDetectBin = origQ, origBin }()

	applang.SetForTest(t, "en")
	runOpencodeQuery = func(_, _ string) ([]byte, error) {
		return []byte(`[
			{"role":"user","text":"打开文件"},
			{"role":"assistant","text":"好的"}
		]`), nil
	}
	opencodeDetectBin = func(string) string { return "/fake/bin" }
	home, _ := os.UserHomeDir()

	md, err := FormatSessionMarkdown(Session{ID: "oc2", ToolID: "opencode", Title: "重构登录页", Workspace: "/w", Path: opencodeDBPath(home)})
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "## User") || !strings.Contains(md, "## Assistant") || !strings.Contains(md, "打开文件") {
		t.Fatalf("应渲染全文轮次, got:\n%s", md)
	}
}

// 零行 / 查询失败 / 解析失败统一是 errOpencodeTranscriptUnavailable（可 errors.Is 命中），
// FormatSessionMarkdown 落回占位文案，绝不渲染其它会话的内容。
func TestFormatSessionMarkdownOpencodeEmptyRowsFallback(t *testing.T) {
	origQ, origBin := runOpencodeQuery, opencodeDetectBin
	defer func() { runOpencodeQuery, opencodeDetectBin = origQ, origBin }()

	applang.SetForTest(t, "en")
	runOpencodeQuery = func(_, _ string) ([]byte, error) {
		return []byte(`[]`), nil
	}
	opencodeDetectBin = func(string) string { return "/fake/bin" }
	home, _ := os.UserHomeDir()

	turns, err := opencodeTranscriptTurns(Session{ID: "oc-empty", ToolID: "opencode", Workspace: "/w", Path: opencodeDBPath(home)})
	if !errors.Is(err, errOpencodeTranscriptUnavailable) {
		t.Fatalf("零行应返回哨兵错误, got %v (turns=%d)", err, len(turns))
	}
	md, err := FormatSessionMarkdown(Session{ID: "oc-empty", ToolID: "opencode", Title: "重构登录页", Workspace: "/w", Path: opencodeDBPath(home)})
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "重构登录页") || !strings.Contains(md, "stored in a database") {
		t.Fatalf("零行应回退占位文案, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownOpencodeDB(t *testing.T) {
	// 正文经 applang 直显：固定 en，避免全局语言态串扰。
	applang.SetForTest(t, "en")
	s := Session{ID: "oc1", ToolID: "opencode", Title: "重构登录页", Path: `D:\fake\opencode.db`}
	md, err := FormatSessionMarkdown(s)
	if err != nil {
		t.Fatalf("sqlite 应返回说明文案而非读库错误: %v", err)
	}
	if !strings.Contains(md, "重构登录页") || !strings.Contains(md, "stored in a database") {
		t.Fatalf("应说明无法展开 sqlite 会话, got:\n%s", md)
	}
}

// 空 Path 是绑定错误（前端 toast），对外为 wire key。
func TestFormatSessionMarkdownNoPath(t *testing.T) {
	_, err := FormatSessionMarkdown(Session{ID: "x", ToolID: "claude"})
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
	md, err := FormatSessionMarkdown(Session{ID: "x", ToolID: "claude", Path: path})
	if err != nil {
		t.Fatalf("FormatSessionMarkdown: %v", err)
	}
	if !strings.Contains(md, "No conversation content could be parsed") {
		t.Fatalf("fallback 正文未本地化, got:\n%s", md)
	}
}

func TestFormatSessionMarkdownMissingFile(t *testing.T) {
	s := Session{ID: "x", ToolID: "claude", Path: "testdata/claude/not-exists.jsonl"}
	if _, err := FormatSessionMarkdown(s); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
