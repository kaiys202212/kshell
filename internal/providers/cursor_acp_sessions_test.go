package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeACPSessionMeta(t *testing.T, home, id, title, cwd string) string {
	t.Helper()
	dir := filepath.Join(home, ".cursor", "acp-sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"cwd":           cwd,
		"title":         title,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeACPSessionWithStore(t *testing.T, home, id, title, cwd string, msgs []map[string]any) string {
	t.Helper()
	writeACPSessionMeta(t, home, id, title, cwd)
	store := filepath.Join(home, ".cursor", "acp-sessions", id, "store.db")
	name := title
	if name == "" {
		name = "Store Fallback"
	}
	writeCursorACPStore(t, store, name, msgs)
	return store
}

func TestListCursorACPSessions(t *testing.T) {
	home := t.TempDir()
	id := "2ce99fcb-7257-4521-bb55-8b9b1a86f48b"
	store := writeACPSessionWithStore(t, home, id, "Activate Session Explain", `d:\data\workspace\moxi\kshell`, []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
	})
	// 无 meta 的目录应跳过
	os.MkdirAll(filepath.Join(home, ".cursor", "acp-sessions", "no-meta"), 0o755)
	// 仅 meta、无 store 的空壳应跳过
	writeACPSessionMeta(t, home, "empty-shell-id", "", `d:\ws`)

	got, err := listCursorACPSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	s := got[0]
	// 标题取首条用户提问，不用 Cursor 自动 meta.title
	if s.ID != id || s.ToolID != cursorID || s.Title != "hello" {
		t.Fatalf("%+v", s)
	}
	// acp-sessions 来源必须标记 ACPOnly：TUI resume 这类 ID 会静默起全新会话丢上下文
	if !s.ACPOnly {
		t.Fatal("acp-sessions 来源的会话应标记 ACPOnly")
	}
	if s.Workspace != `d:\data\workspace\moxi\kshell` {
		t.Fatalf("workspace %q", s.Workspace)
	}
	if s.Path != store {
		t.Fatalf("Path=%q want store.db", s.Path)
	}
	if s.Messages != 2 {
		t.Fatalf("Messages=%d", s.Messages)
	}
	st, _ := os.Stat(store)
	if s.UpdatedAt.IsZero() || s.UpdatedAt.Sub(st.ModTime()).Abs() > time.Second {
		t.Fatalf("UpdatedAt=%v store mtime=%v", s.UpdatedAt, st.ModTime())
	}
}

func TestEnumerateSessionsIncludesACPSessions(t *testing.T) {
	home := t.TempDir()
	acpID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	writeACPSessionWithStore(t, home, acpID, "ACP Only", `D:\ws`, []map[string]any{
		{"role": "user", "content": "q"},
		{"role": "assistant", "content": "a"},
	})
	// chats 里另有一条，Enumerate 应两者都有且 chats 优先保留
	chatID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", "h", chatID), true, "From Chats", `D:\ws`)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Session{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if byID[acpID].Title != "q" {
		t.Fatalf("missing acp session: %+v", got)
	}
	if byID[chatID].Title != "From Chats" {
		t.Fatalf("missing chats session: %+v", got)
	}
	// acp-sessions 来源带 ACPOnly 标记；chats 来源绝不能带（否则会被误禁终端）
	if !byID[acpID].ACPOnly {
		t.Fatalf("acp-sessions 来源应带 ACPOnly 标记: %+v", byID[acpID])
	}
	if byID[chatID].ACPOnly {
		t.Fatalf("chats 来源不应带 ACPOnly 标记: %+v", byID[chatID])
	}
}

func TestEnumerateSessionsPrefersChatsWhenSameID(t *testing.T) {
	home := t.TempDir()
	id := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", "h", id), true, "Chats Title", `D:\ws`)
	writeACPSessionWithStore(t, home, id, "ACP Title", `D:\other`, []map[string]any{
		{"role": "user", "content": "q"},
		{"role": "assistant", "content": "a"},
	})

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	var hit *Session
	for i := range got {
		if got[i].ID == id {
			hit = &got[i]
			break
		}
	}
	if hit == nil || hit.Title != "Chats Title" || hit.Workspace != `D:\ws` {
		t.Fatalf("同 ID 应保留 chats 条目: %+v", hit)
	}
	// chats 优先保留的条目同样不能带 ACPOnly 标记
	if hit.ACPOnly {
		t.Fatalf("同 ID 保留的 chats 条目不应带 ACPOnly 标记: %+v", hit)
	}
}
