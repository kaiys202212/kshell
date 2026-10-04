package providers

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func writeMeta(t *testing.T, dir string, hasConversation bool, title, cwd string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(cursorChatMeta{
		SchemaVersion:   1,
		CreatedAtMs:     1000,
		UpdatedAtMs:     2000,
		HasConversation: hasConversation,
		Title:           title,
		Cwd:             cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCursorChatMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")
	os.WriteFile(path, []byte(`{
		"schemaVersion":1,
		"createdAtMs":1000,
		"updatedAtMs":2000,
		"hasConversation":true,
		"title":"Hello",
		"cwd":"D:\\proj"
	}`), 0o600)
	got, err := loadCursorChatMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasConversation || got.Title != "Hello" || got.Cwd != `D:\proj` {
		t.Fatalf("%+v", got)
	}
	if !got.CreatedAt().Equal(time.UnixMilli(1000)) {
		t.Fatalf("created %v", got.CreatedAt())
	}
}

func TestListCursorChatSessionsSkipsSubagentsAndEmpty(t *testing.T) {
	home := t.TempDir()
	hash := "abc"
	base := filepath.Join(home, ".cursor", "chats", hash)
	// 主会话
	mainID := "11111111-1111-4111-8111-111111111111"
	writeMeta(t, filepath.Join(base, mainID), true, "Main", `D:\ws`)
	// 子代理：无 meta
	subID := "22222222-2222-4222-8222-222222222222"
	os.MkdirAll(filepath.Join(base, subID), 0o755)
	os.WriteFile(filepath.Join(base, subID, "store.db"), []byte("x"), 0o600)
	// hasConversation=false
	emptyID := "33333333-3333-4333-8333-333333333333"
	writeMeta(t, filepath.Join(base, emptyID), false, "", `D:\ws`)

	got, err := listCursorChatSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID || got[0].Title != "Main" || got[0].Workspace != `D:\ws` {
		t.Fatalf("%+v", got)
	}
	if got[0].ToolID != cursorID {
		t.Fatalf("tool %q", got[0].ToolID)
	}
}

func writeStoreMeta(t *testing.T, dbPath, jsonBody string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	hexVal := hex.EncodeToString([]byte(jsonBody))
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?)`, hexVal); err != nil {
		t.Fatal(err)
	}
}

func TestCursorStoreHasSubagentInfo(t *testing.T) {
	dir := t.TempDir()
	subDB := filepath.Join(dir, "sub.db")
	writeStoreMeta(t, subDB, `{"agentId":"a","name":"New Agent","subagentInfo":{"parentAgentId":"p","typeName":"generalPurpose"}}`)
	has, err := cursorStoreHasSubagentInfo(subDB)
	if err != nil || !has {
		t.Fatalf("has=%v err=%v, want true", has, err)
	}

	mainDB := filepath.Join(dir, "main.db")
	writeStoreMeta(t, mainDB, `{"agentId":"b","name":"Main Chat"}`)
	has, err = cursorStoreHasSubagentInfo(mainDB)
	if err != nil || has {
		t.Fatalf("has=%v err=%v, want false", has, err)
	}

	nullDB := filepath.Join(dir, "null.db")
	writeStoreMeta(t, nullDB, `{"agentId":"c","subagentInfo":null}`)
	has, err = cursorStoreHasSubagentInfo(nullDB)
	if err != nil || has {
		t.Fatalf("null subagentInfo: has=%v err=%v, want false", has, err)
	}

	strDB := filepath.Join(dir, "str.db")
	writeStoreMeta(t, strDB, `{"agentId":"d","subagentInfo":"not-an-object"}`)
	has, err = cursorStoreHasSubagentInfo(strDB)
	if err != nil || has {
		t.Fatalf("string subagentInfo: has=%v err=%v, want false", has, err)
	}

	_, err = cursorStoreHasSubagentInfo(filepath.Join(dir, "missing.db"))
	if err == nil {
		t.Fatal("want error for missing db")
	}
}
