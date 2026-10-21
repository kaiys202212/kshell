package providers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeCursorACPStore 写入最小可用的 Cursor ACP store.db：
// root protobuf 的 field1 链按 msgs 顺序引用各 JSON blob。
func writeCursorACPStore(t *testing.T, dbPath string, storeName string, msgs []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT, value TEXT)`); err != nil {
		t.Fatal(err)
	}

	var root []byte
	var firstID string
	for i, msg := range msgs {
		raw, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		id := hex.EncodeToString(sum[:])
		if i == 0 {
			firstID = id
		}
		if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`, id, raw); err != nil {
			t.Fatal(err)
		}
		// protobuf: field 1, wire 2, len 32, 32-byte id
		root = append(root, 0x0a, 0x20)
		root = append(root, sum[:]...)
	}
	rootSum := sha256.Sum256(root)
	rootID := hex.EncodeToString(rootSum[:])
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, ?)`, rootID, root); err != nil {
		t.Fatal(err)
	}
	metaObj := map[string]any{
		"agentId":           "test-agent",
		"latestRootBlobId":  rootID,
		"name":              storeName,
		"createdAt":         int64(1_700_000_000_000),
	}
	metaJSON, err := json.Marshal(metaObj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?)`, hex.EncodeToString(metaJSON)); err != nil {
		t.Fatal(err)
	}
	_ = firstID
}

func TestTurnsFromCursorStoreOrderedUserAssistant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	writeCursorACPStore(t, dbPath, "Store Name", []map[string]any{
		{"role": "system", "content": "You are a coding agent"},
		{"role": "user", "content": "<user_info>ctx</user_info><git_status>M x</git_status>"},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "<timestamp>Tue</timestamp>\n<user_query>修复登录空指针</user_query>"},
		}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "reasoning", "text": "think"},
			map[string]any{"type": "text", "text": "我来定位空指针。"},
			map[string]any{"type": "tool-call", "toolName": "Grep"},
		}},
		{"role": "tool", "content": []any{
			map[string]any{"type": "tool-result", "toolName": "Grep"},
		}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "tool-call", "toolName": "Read"},
		}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "已修好。"},
		}},
	})

	turns, meta, err := turnsFromCursorStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Store Name" {
		t.Fatalf("meta.Name=%q", meta.Name)
	}
	// system / 上下文注入 user / tool / 无 text 的 assistant 都应跳过
	if len(turns) != 3 {
		t.Fatalf("turns=%d %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || turns[0].Text != "修复登录空指针" {
		t.Fatalf("turn0=%+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Text != "我来定位空指针。" {
		t.Fatalf("turn1=%+v", turns[1])
	}
	if turns[2].Role != "assistant" || turns[2].Text != "已修好。" {
		t.Fatalf("turn2=%+v", turns[2])
	}
}

func TestTurnsFromCursorStoreMissing(t *testing.T) {
	_, _, err := turnsFromCursorStore(filepath.Join(t.TempDir(), "nope.db"))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestListCursorACPSessionsSkipsEmptyAndEnrichesFromStore(t *testing.T) {
	home := t.TempDir()
	emptyID := "11111111-1111-4111-8111-111111111111"
	writeACPSessionMeta(t, home, emptyID, "", `D:\ws`)

	fullID := "22222222-2222-4222-8222-222222222222"
	writeACPSessionMeta(t, home, fullID, "", `D:\ws`) // 无 title，应从 store name / 用户消息回退
	store := filepath.Join(home, ".cursor", "acp-sessions", fullID, "store.db")
	writeCursorACPStore(t, store, "From Store Name", []map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "<user_query>真实问题标题</user_query>"},
		}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "答"},
		}},
	})

	got, err := listCursorACPSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("应跳过空壳, len=%d %+v", len(got), got)
	}
	s := got[0]
	if s.ID != fullID {
		t.Fatalf("%+v", s)
	}
	if s.Path != store {
		t.Fatalf("Path=%q want store.db", s.Path)
	}
	if s.Messages != 2 {
		t.Fatalf("Messages=%d want 2", s.Messages)
	}
	// meta.json 无 title 时优先 store name
	if s.Title != "From Store Name" {
		t.Fatalf("Title=%q", s.Title)
	}
	if !s.ACPOnly {
		t.Fatal("want ACPOnly")
	}
}

func TestListCursorACPSessionsPrefersMetaTitle(t *testing.T) {
	home := t.TempDir()
	id := "33333333-3333-4333-8333-333333333333"
	writeACPSessionMeta(t, home, id, "Meta Title", `D:\ws`)
	store := filepath.Join(home, ".cursor", "acp-sessions", id, "store.db")
	writeCursorACPStore(t, store, "Store Name", []map[string]any{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "yo"},
	})
	got, err := listCursorACPSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Meta Title" {
		t.Fatalf("%+v", got)
	}
}

func TestFormatSessionMarkdownCursorACPStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	writeCursorACPStore(t, dbPath, "Preview Title", []map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "<user_query>预览用户问题</user_query>"},
		}},
		{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "预览助手回答"},
		}},
	})
	md, err := FormatSessionMarkdown(Session{
		ID:     "acp1",
		ToolID: cursorID,
		Title:  "Preview Title",
		Path:   dbPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "stored in a database") || strings.Contains(md, "保存在数据库") {
		t.Fatalf("不应走 sqlite 占位文案, got:\n%s", md)
	}
	if !strings.Contains(md, "预览用户问题") || !strings.Contains(md, "预览助手回答") {
		t.Fatalf("应还原对话正文, got:\n%s", md)
	}
}
