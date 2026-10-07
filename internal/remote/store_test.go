package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
}

// store 相关错误一律以 wire key 暴露给前端翻译。
func TestStoreErrorsWireKeys(t *testing.T) {
	if errMissingHost.Error() != "err.ssh.missing_host" {
		t.Fatalf("host key = %q", errMissingHost.Error())
	}
	if errKeyMaterial.Error() != "err.ssh.identity_file_path_only" {
		t.Fatalf("key key = %q", errKeyMaterial.Error())
	}
	if ErrUnknownConnection.Error() != "err.ssh.not_in_store" {
		t.Fatalf("store key = %q", ErrUnknownConnection.Error())
	}
}

// 连接文件解析失败也走 wire key。
func TestLoadParseFailureWireKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "connections.yaml")
	if err := os.WriteFile(p, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewStore(p).Load()
	if err == nil {
		t.Fatal("非法 YAML 应报错")
	}
	if !strings.HasPrefix(err.Error(), "err.ssh.parse_file_failed|") {
		t.Fatalf("err = %q", err.Error())
	}
}

func TestAddAndRoundTrip(t *testing.T) {
	store := tempStore(t)
	added, err := store.Add(Connection{Name: "prod", Host: "10.0.0.1", User: "root", Port: 2222, Workspace: "D:\\ws-a"})
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if added.ID == "" {
		t.Fatal("Add should generate an id")
	}

	reloaded := NewStore(store.Path())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	conns := reloaded.All()
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	if conns[0].Host != "10.0.0.1" || conns[0].User != "root" || conns[0].Port != 2222 {
		t.Fatalf("round trip lost data: %+v", conns[0])
	}
	if conns[0].Workspace != "D:\\ws-a" {
		t.Fatalf("workspace binding lost: %+v", conns[0])
	}
}

func TestAddRejectsMissingHost(t *testing.T) {
	store := tempStore(t)
	if _, err := store.Add(Connection{Name: "nohost"}); err == nil {
		t.Fatal("connection without host must be rejected")
	}
}

func TestPortDefaultsTo22(t *testing.T) {
	store := tempStore(t)
	added, _ := store.Add(Connection{Name: "x", Host: "h"})
	if added.Port != 22 {
		t.Fatalf("port = %d, want 22", added.Port)
	}
}

func TestListFiltersByWorkspace(t *testing.T) {
	store := tempStore(t)
	mustAdd(t, store, Connection{Name: "ws-a-conn", Host: "a", Workspace: "D:\\ws-a"})
	mustAdd(t, store, Connection{Name: "ws-b-conn", Host: "b", Workspace: "D:\\ws-b"})
	mustAdd(t, store, Connection{Name: "global", Host: "g"})

	got := store.List("D:\\ws-a")
	names := []string{}
	for _, c := range got {
		names = append(names, c.Name)
	}
	if len(names) != 2 {
		t.Fatalf("names = %v, want ws-a-conn + global", names)
	}
	for _, n := range names {
		if n == "ws-b-conn" {
			t.Fatalf("other workspaces must not leak in: %v", names)
		}
	}
}

func TestDelete(t *testing.T) {
	store := tempStore(t)
	added, _ := store.Add(Connection{Name: "gone", Host: "h"})
	if err := store.Delete(added.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if len(store.All()) != 0 {
		t.Fatalf("store should be empty, got %+v", store.All())
	}
	if err := store.Delete("nope"); err == nil {
		t.Fatal("deleting an unknown id should fail")
	}
}

func TestConnectionPasswordRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "connections.yaml"))
	_, err := store.Add(Connection{Name: "p", Host: "h", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	all := store.All()
	if len(all) != 1 || all[0].Password != "s3cret" {
		t.Fatalf("got %+v", all)
	}
}

func TestNoSecretContentPersisted(t *testing.T) {
	store := tempStore(t)
	_, err := store.Add(Connection{
		Name:         "prod",
		Host:         "10.0.0.1",
		IdentityFile: "C:\\Users\\me\\.ssh\\id_ed25519",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "id_ed25519") {
		t.Fatal("identity path should be stored")
	}
	// 私钥材料仍禁止落盘；明文密码由 TestConnectionPasswordRoundTrip 覆盖。
	if strings.Contains(content, "PRIVATE KEY") {
		t.Fatalf("secrets must never be persisted, found %q", "PRIVATE KEY")
	}
}

func TestAddRejectsKeyMaterialAsIdentity(t *testing.T) {
	store := tempStore(t)
	_, err := store.Add(Connection{Name: "bad", Host: "h", IdentityFile: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc"})
	if err == nil {
		t.Fatal("identity file must be a path, not key material")
	}
}

func TestAtomicWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "connections.yaml"))
	mustAdd(t, store, Connection{Name: "a", Host: "a"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store := tempStore(t)
	if err := store.Load(); err != nil {
		t.Fatalf("loading a missing file must not fail: %v", err)
	}
	if len(store.All()) != 0 {
		t.Fatal("expected no connections")
	}
}

func mustAdd(t *testing.T, store *Store, c Connection) Connection {
	t.Helper()
	added, err := store.Add(c)
	if err != nil {
		t.Fatalf("Add(%+v): %v", c, err)
	}
	return added
}
