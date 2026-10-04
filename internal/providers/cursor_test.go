package providers

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// Cursor 用户消息常带 <timestamp>Sunday, ...</timestamp> 前缀；标题必须取
// <user_query> 正文，不能把星期几当页签名。
func TestCursorParseSessionStripsTimestampPrefix(t *testing.T) {
	p := Cursor{}
	const uuid = "4a5b6666-7777-4888-8999-aaaabbbbcccc"
	path := cursorPath("d-data-workspace-moxi-kshell", uuid)

	got, err := p.ParseSession(path, cursorFixture(t, "d-data-workspace-moxi-kshell", uuid))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "几个 bug 需要修复下" {
		t.Fatalf("title = %q, want 剥掉 timestamp 后的 user_query 正文", got.Title)
	}
	if strings.Contains(strings.ToLower(got.Title), "sunday") {
		t.Fatalf("title 不应残留星期：%q", got.Title)
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

func TestCursorEnumerateFromChats(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "ws")
	os.MkdirAll(ws, 0o755)
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0001"
	hash := "deadbeef"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", hash, id), true, "FromMeta", ws)
	slug := (Cursor{}).WorkspaceToSlug(ws)
	trDir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(trDir, 0o755)
	body := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>修复登录页空指针</user_query>"}]}}` + "\n"
	os.WriteFile(filepath.Join(trDir, id+".jsonl"), []byte(body), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "修复登录页空指针" {
		t.Fatalf("title=%q, want transcript 优先于 meta", got[0].Title)
	}
	if got[0].Path == "" || got[0].Messages < 1 {
		t.Fatalf("path/messages %+v", got[0])
	}
}

func TestCursorEnumerateKeepsMetaWhenNoTranscript(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "ws")
	os.MkdirAll(ws, 0o755)
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0099"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", "h", id), true, "OnlyMeta", ws)
	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "OnlyMeta" {
		t.Fatalf("%+v", got)
	}
}

func TestCursorEnumerateHidesStoreOnlySubagent(t *testing.T) {
	home := t.TempDir()
	hash := "h1"
	mainID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0002"
	subID := "bbbbbbbb-cccc-4ddd-8eee-ffff00001111"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", hash, mainID), true, "M", `D:\w`)
	subDir := filepath.Join(home, ".cursor", "chats", hash, subID)
	os.MkdirAll(subDir, 0o755)
	writeStoreMeta(t, filepath.Join(subDir, "store.db"), `{"subagentInfo":{"parentAgentId":"x"}}`)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID {
		t.Fatalf("%+v", got)
	}
}

func TestCursorEnumerateFallbackFiltersSubagents(t *testing.T) {
	home := t.TempDir()
	slug := "d-ws"
	mainID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0101"
	subID := "bbbbbbbb-cccc-4ddd-8eee-ffff00000202"
	os.MkdirAll(filepath.Join(home, ".cursor", "chats", "h", subID), 0o755)

	writeTranscript := func(id, title string) {
		dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
		os.MkdirAll(dir, 0o755)
		line := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>` + title + `</user_query>"}]}}` + "\n"
		os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o600)
	}
	writeTranscript(mainID, "real user")
	writeTranscript(subID, "You are implementing Task 1")
	os.MkdirAll(filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", mainID, "subagents"), 0o755)
	os.WriteFile(filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", mainID, "subagents", "agent-x.jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"x"}]}}`+"\n"), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID {
		t.Fatalf("%+v", got)
	}
}

func TestCursorEnumerateFallbackKeepsWhenNoChatsEntry(t *testing.T) {
	home := t.TempDir()
	id := "cccccccc-dddd-4eee-8fff-000011112222"
	slug := "d-ws"
	dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>keep me</user_query>"}]}}`+"\n"), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("%+v", got)
	}
}

func TestCursorEnumerateFallbackSkipsChatsDirWithoutMeta(t *testing.T) {
	home := t.TempDir()
	slug := "d-ws"
	id := "dddddddd-eeee-4fff-8aaa-111122223333"
	os.MkdirAll(filepath.Join(home, ".cursor", "chats", "h", id), 0o755)
	os.WriteFile(filepath.Join(home, ".cursor", "chats", "h", id, "store.db"), []byte("not sqlite"), 0o600)
	dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>hidden</user_query>"}]}}`+"\n"), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want skip, got %+v", got)
	}
}

func TestCursorEnumerateFallbackNoChatsDir(t *testing.T) {
	home := t.TempDir()
	slug := "d-ws"
	id := "eeeeeeee-ffff-4aaa-8bbb-222233334444"
	dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>only transcript</user_query>"}]}}`+"\n"), 0o600)
	os.MkdirAll(filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id, "subagents"), 0o755)
	os.WriteFile(filepath.Join(dir, "subagents", "agent-y.jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"sub"}]}}`+"\n"), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("%+v", got)
	}
}

func TestCursorResumeCmd(t *testing.T) {
	p := Cursor{}

	// 解码出的工作区真实存在 → 用作启动目录
	realDir := t.TempDir()
	got := p.ResumeCmd(Session{ID: "uuid-9", ToolID: "cursor", Workspace: realDir}, "cursor-agent")
	if got.Path != "cursor-agent" {
		t.Fatalf("path = %q", got.Path)
	}
	if strings.Join(got.Args, " ") != "--resume uuid-9" {
		t.Fatalf("args = %v", got.Args)
	}
	if got.Dir != realDir {
		t.Fatalf("dir = %q, want %q", got.Dir, realDir)
	}

	// slug 有损逆解码可能解出不存在的目录 → 放弃 Dir，交给 launcher 回退
	got = p.ResumeCmd(Session{ID: "uuid-9", ToolID: "cursor", Workspace: `d:\no\such\dir`}, "cursor-agent")
	if got.Dir != "" {
		t.Fatalf("dir = %q, want 空串（目录不存在时回退）", got.Dir)
	}
}

func TestCursorDetectFindsInstallDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	spec := Cursor{}.DetectSpec(home)
	if len(spec.InstallDirs) == 0 {
		t.Fatal("cursor 必须声明 InstallDirs")
	}

	var dir string
	if runtime.GOOS == "windows" {
		local := t.TempDir()
		t.Setenv("LOCALAPPDATA", local)
		dir = filepath.Join(local, "cursor-agent")
	} else {
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeBin(t, dir, "cursor-agent", "echo cursor-agent")

	got := Detect(spec, home)
	if !got.Installed || got.Source != "install-dir" {
		t.Fatalf("got %+v, want install-dir", got)
	}
}

func TestCursorDetectFindsVersionedInstallDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("官方 Windows 安装才把 CLI 放在 versions\\<ver>\\")
	}
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	verDir := filepath.Join(local, "cursor-agent", "versions", "2026.10.01-e373342")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := writeFakeBin(t, verDir, "cursor-agent", "echo cursor-agent")

	got := Detect(Cursor{}.DetectSpec(home), home)
	if !got.Installed || got.Source != "install-dir" {
		t.Fatalf("got %+v, want install-dir via versions", got)
	}
	if filepath.Clean(got.BinPath) != filepath.Clean(bin) {
		t.Fatalf("BinPath = %q, want %q", got.BinPath, bin)
	}
}
