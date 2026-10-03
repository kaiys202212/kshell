package providers

import (
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
