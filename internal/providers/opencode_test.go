package providers

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 单行是硬约束：.cmd 包装层的命令行会被 cmd.exe 重新解析，参数里的换行会被截断
// （实测多行 SQL 时 opencode db 报 `no such column: id`，会话一条都查不出来）。
func TestOpencodeSessionsSQLIsSingleLine(t *testing.T) {
	if strings.ContainsAny(opencodeSessionsSQL, "\r\n") {
		t.Fatal("opencode 查询 SQL 不能带换行")
	}
	if !strings.Contains(opencodeSessionsSQL, "parent_id is null") ||
		!strings.Contains(opencodeSessionsSQL, "time_archived is null") {
		t.Fatalf("SQL 语义被改动: %q", opencodeSessionsSQL)
	}
	// 标题兜底与消息条数两列：缺了会退化回 "New session" 展示（见 parse 用例）
	if !strings.Contains(opencodeSessionsSQL, `like 'New session%'`) ||
		!strings.Contains(opencodeSessionsSQL, " as messages") ||
		!strings.Contains(opencodeSessionsSQL, " as fallback") {
		t.Fatalf("SQL 缺少 messages/fallback 列: %q", opencodeSessionsSQL)
	}
	// preview 列：列表级内容预览（首条真实用户消息，过滤 synthetic）
	if !strings.Contains(opencodeSessionsSQL, " as preview") ||
		!strings.Contains(opencodeSessionsSQL, "'$.synthetic') is null") {
		t.Fatalf("SQL 缺少 preview 列: %q", opencodeSessionsSQL)
	}
	// 空会话过滤：零消息且懒默认标题（New session…）的行被排除；
	// legacy SQL 是结构降级路径，语义不得改动，必须不含该过滤。
	if !strings.Contains(opencodeSessionsSQL, "title not like 'New session%'") ||
		!strings.Contains(opencodeSessionsSQL, ") > 0") {
		t.Fatalf("SQL 缺少空会话过滤子句: %q", opencodeSessionsSQL)
	}
	if strings.Contains(legacyOpencodeSessionsSQL, "not like") {
		t.Fatalf("legacy SQL 不应含空会话过滤: %q", legacyOpencodeSessionsSQL)
	}
}

// TestOpencodeParsePreview：preview 列进 Session.Preview；换行压成单行并截断
// 到 160 字符，列缺失（legacy 降级行）保持零值。
func TestOpencodeParsePreview(t *testing.T) {
	// 200 个汉字：SQL 层 substr(400) 不截，Go 层 oneLine(160) 截断
	s := strings.Repeat("汉", 200)
	body, err := json.Marshal([]map[string]any{
		{"id": "ses_prev", "cwd": "D:/ws", "title": "t1", "created": 1, "updated": 2, "preview": s},
		{"id": "ses_none", "cwd": "D:/ws", "title": "t2", "created": 1, "updated": 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := parseOpencodeSessions(body, "db")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := string([]rune(s)[:160]) + "…"
	if sessions[0].Preview != want {
		t.Fatalf("preview = %q, want %q", sessions[0].Preview, want)
	}
	if sessions[1].Preview != "" {
		t.Fatalf("preview 列缺失应取零值: %q", sessions[1].Preview)
	}
}

// TestOpencodeParsePreviewFlattensAndTruncates：preview 是多行文本时压成单行。
func TestOpencodeParsePreviewFlattensAndTruncates(t *testing.T) {
	body, err := json.Marshal([]map[string]any{
		{"id": "ses_ml", "cwd": "D:/ws", "title": "t", "created": 1, "updated": 2,
			"preview": "第一行\n第二行\t第三行"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := parseOpencodeSessions(body, "db")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sessions[0].Preview != "第一行 第二行 第三行" {
		t.Fatalf("preview = %q, want 单行", sessions[0].Preview)
	}
}

func TestOpencodeParseHomesMessagesAndFallback(t *testing.T) {
	raw := []byte(`[
		{"id":"ses_new","cwd":"D:/ws","title":"New session - 2026-10-31T15:42:42.197Z","created":1759384800000,"updated":1759388400000,"messages":1,"fallback":"修复登录空指针"},
		{"id":"ses_plain","cwd":"D:/ws","title":"业务标题","created":1759384800000,"updated":1759388400000,"messages":44,"fallback":"不应采用"},
		{"id":"ses_old","cwd":"D:/ws","title":"New session - 2026-06-10T09:10:24.230Z","created":1759384800000,"updated":1759388400000},
		{"id":"ses_empty","cwd":"D:/ws","title":"New session - 2026-10-31T14:45:12.367Z","created":1759384800000,"updated":1759388400000,"messages":0,"fallback":""},
		{"id":"ses_wrapped","cwd":"D:/ws","title":"New session - 2026-10-31T13:00:00.000Z","created":1759384800000,"updated":1759388400000,"messages":2,"fallback":"<user_query>关注性能</user_query>\n第二行不加"}
	]`)

	sessions, err := parseOpencodeSessions(raw, `C:\home\.local\share\opencode\opencode.db`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sessions) != 5 {
		t.Fatalf("len = %d, want 5", len(sessions))
	}

	// New session + 首条用户消息 → 兜底标题
	if sessions[0].Title != "修复登录空指针" {
		t.Fatalf("fallback title = %q", sessions[0].Title)
	}
	// 正常标题（即使有 fallback）不覆盖
	if sessions[1].Title != "业务标题" {
		t.Fatalf("plain title = %q", sessions[1].Title)
	}
	if sessions[1].Messages != 44 || sessions[0].Messages != 1 {
		t.Fatalf("messages = %d/%d, want 44/1", sessions[1].Messages, sessions[0].Messages)
	}
	// 旧版本输出无这批键：零值兼容，标题维持库里默认（枚举本身不失败）
	if sessions[2].Messages != 0 || !strings.HasPrefix(sessions[2].Title, "New session") {
		t.Fatalf("旧输出兼容 = %+v", sessions[2])
	}
	// New session 但库里没有任何用户消息：保持原标题，不得变空
	if sessions[3].Title != "New session - 2026-10-31T14:45:12.367Z" {
		t.Fatalf("empty fallback title = %q", sessions[3].Title)
	}
	// fallback 经 ClipPromptTitle 清洗：只取第一行、剥包装标签
	if sessions[4].Title != "关注性能" {
		t.Fatalf("wrapped fallback title = %q", sessions[4].Title)
	}
}

func TestOpencodeParseSessions(t *testing.T) {
	raw := []byte(`[
		{"id":"ses_a","cwd":"D:/data/workspace/moxi/kshell","title":"修复 扫描","created":1759384800000,"updated":1759388400000},
		{"id":"ses_b","cwd":"D:/data/workspace/moxi/recorder","title":"","created":1759000000000,"updated":0}
	]`)

	sessions, err := parseOpencodeSessions(raw, `C:\home\.local\share\opencode\opencode.db`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2", len(sessions))
	}

	got := sessions[0]
	if got.ID != "ses_a" || got.ToolID != "opencode" {
		t.Fatalf("id/tool = %q/%q", got.ID, got.ToolID)
	}
	// 库里的前斜杠要归一到平台写法（Windows 上其它工具存的是 `D:\...`）
	if got.Workspace != filepath.Clean("D:/data/workspace/moxi/kshell") {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "修复 扫描" {
		t.Fatalf("title = %q", got.Title)
	}
	if !got.CreatedAt.Equal(time.UnixMilli(1759384800000)) {
		t.Fatalf("createdAt = %v", got.CreatedAt)
	}
	if !got.UpdatedAt.Equal(time.UnixMilli(1759388400000)) {
		t.Fatalf("updatedAt = %v", got.UpdatedAt)
	}
	if !strings.HasSuffix(got.Path, filepath.Join(".local", "share", "opencode", "opencode.db")) {
		t.Fatalf("path = %q", got.Path)
	}

	// updated 缺失时退回 created，避免列表里时间显示为零值。
	if !sessions[1].UpdatedAt.Equal(time.UnixMilli(1759000000000)) {
		t.Fatalf("fallback updatedAt = %v", sessions[1].UpdatedAt)
	}
}

func TestOpencodeParseDropsOSCColorReplyTitle(t *testing.T) {
	raw := []byte(`[
		{"id":"ses_rgb","cwd":"D:/ws","title":"4;0;rgb:2e2e/3434/3636","created":1,"updated":2}
	]`)
	sessions, err := parseOpencodeSessions(raw, "db")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Title != "" {
		t.Fatalf("title = %q, want empty after cleanTitle", sessions[0].Title)
	}
}

func TestOpencodeParseSkipsRowsWithoutWorkspace(t *testing.T) {
	raw := []byte(`[
		{"id":"","cwd":"D:/x","title":"无 id"},
		{"id":"ses_c","cwd":"","title":"无目录"},
		{"id":"ses_d","cwd":"   ","title":"空目录"},
		{"id":"ses_e","cwd":"D:/y","title":"好的"}
	]`)

	sessions, err := parseOpencodeSessions(raw, "db")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_e" {
		t.Fatalf("sessions = %+v, want only ses_e", sessions)
	}
}

func TestOpencodeParseBadJSON(t *testing.T) {
	if _, err := parseOpencodeSessions([]byte("boom"), "db"); err == nil {
		t.Fatal("want error for non-JSON output")
	}
}

func TestOpencodeEnumerateWithoutBin(t *testing.T) {
	orig := runOpencodeQuery
	defer func() { runOpencodeQuery = orig }()

	called := false
	runOpencodeQuery = func(bin, query string) ([]byte, error) {
		called = true
		return nil, errors.New("不该被调用")
	}

	sessions, err := Opencode{}.EnumerateSessions(`C:\home`, "")
	if err != nil || sessions != nil {
		t.Fatalf("sessions = %v, err = %v; want nil, nil", sessions, err)
	}
	if called {
		t.Fatal("bin 为空时必须跳过查询")
	}
}

func TestOpencodeEnumeratePropagatesError(t *testing.T) {
	orig := runOpencodeQuery
	defer func() { runOpencodeQuery = orig }()

	runOpencodeQuery = func(bin, query string) ([]byte, error) {
		if !strings.Contains(query, "from session") {
			t.Fatalf("query = %q", query)
		}
		return nil, errors.New("db 查询失败")
	}

	if _, err := (Opencode{}).EnumerateSessions(`C:\home`, "opencode"); err == nil {
		t.Fatal("want error")
	}
}

func TestOpencodeEnumerateFallsBackToLegacySQL(t *testing.T) {
	orig := runOpencodeQuery
	defer func() { runOpencodeQuery = orig }()

	var calls []string
	runOpencodeQuery = func(bin, query string) ([]byte, error) {
		calls = append(calls, query)
		if strings.Contains(query, " as fallback") {
			// 新 SQL 第一跳失败（模拟老版本 db 结构差异）
			return nil, errors.New("opencode db 退出码 1: no such column")
		}
		return []byte(`[{"id":"ses_a","cwd":"/w","title":"t","created":1,"updated":2}]`), nil
	}

	sessions, err := Opencode{}.EnumerateSessions("/home/me", "/usr/bin/opencode")
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("queries = %d (%q)", len(calls), calls)
	}
	if strings.Contains(calls[1], " as fallback") {
		t.Fatalf("重试仍用新 SQL: %q", calls[1])
	}
	if len(sessions) != 1 || sessions[0].ToolID != "opencode" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestOpencodeEnumerateHappyPath(t *testing.T) {
	orig := runOpencodeQuery
	defer func() { runOpencodeQuery = orig }()

	var gotBin, gotQuery string
	runOpencodeQuery = func(bin, query string) ([]byte, error) {
		gotBin, gotQuery = bin, query
		return []byte(`[{"id":"ses_a","cwd":"/w","title":"t","created":1,"updated":2}]`), nil
	}

	sessions, err := Opencode{}.EnumerateSessions("/home/me", "/usr/bin/opencode")
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if gotBin != "/usr/bin/opencode" {
		t.Fatalf("bin = %q", gotBin)
	}
	if !strings.Contains(gotQuery, "parent_id is null") || !strings.Contains(gotQuery, "time_archived is null") {
		t.Fatalf("query should filter root sessions: %q", gotQuery)
	}
	if len(sessions) != 1 || sessions[0].ToolID != "opencode" {
		t.Fatalf("sessions = %+v", sessions)
	}
	if sessions[0].Path != filepath.Join("/home/me", ".local", "share", "opencode", "opencode.db") {
		t.Fatalf("path = %q", sessions[0].Path)
	}
}

func TestOpencodeResumeAndNewSession(t *testing.T) {
	resume := Opencode{}.ResumeCmd(Session{ID: "ses_x", Workspace: "/w"}, "opencode")
	if len(resume.Args) != 2 || resume.Args[0] != "--session" || resume.Args[1] != "ses_x" {
		t.Fatalf("resume args = %v", resume.Args)
	}
	if resume.Dir != "/w" {
		t.Fatalf("resume dir = %q", resume.Dir)
	}

	fresh := Opencode{}.NewSessionCmd("/w", "opencode")
	if len(fresh.Args) != 0 || fresh.Dir != "/w" {
		t.Fatalf("new session = %+v", fresh)
	}
}

func TestOpencodeIsNotFileBased(t *testing.T) {
	if roots := (Opencode{}).SessionRoots("/home/me"); roots != nil {
		t.Fatalf("roots = %v, want nil", roots)
	}
	if _, err := (Opencode{}).ParseSession("x.json", nil); err == nil {
		t.Fatal("ParseSession 不该被调用，应返回错误")
	}
}
