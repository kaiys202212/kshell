package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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
