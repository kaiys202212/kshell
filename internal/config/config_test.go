package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tempPaths(t *testing.T) Layout {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	p, err := Paths()
	if err != nil {
		t.Fatalf("Paths() error: %v", err)
	}
	return p
}

func TestPathsUnderHome(t *testing.T) {
	p := tempPaths(t)
	if filepath.Base(p.Root) != ".kshell" {
		t.Fatalf("root = %s, want .kshell", p.Root)
	}
	if p.Config != filepath.Join(p.Root, "config.yaml") {
		t.Fatalf("config = %s", p.Config)
	}
	if p.Connections != filepath.Join(p.Root, "connections.yaml") {
		t.Fatalf("connections = %s", p.Connections)
	}
	if p.Providers != filepath.Join(p.Root, "providers.yaml") {
		t.Fatalf("providers = %s", p.Providers)
	}
	if p.CacheIndex != filepath.Join(p.Root, "cache", "index.json") {
		t.Fatalf("cache index = %s", p.CacheIndex)
	}
}

func TestLoadDefaultWhenMissing(t *testing.T) {
	p := tempPaths(t)
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v, want %+v", got, Default())
	}
}

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	p := tempPaths(t)
	want := Default()
	want.MaxDepth = 3
	want.ScanRoots = []string{filepath.Join("D:", "data", "workspace")}

	if err := Save(p, want); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}

	entries, err := os.ReadDir(p.Root)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("atomic save left temp file: %s", e.Name())
		}
	}
}

func TestLoadRecoversFromCorruptConfig(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Config, []byte("scan_roots: [unterminated\n  - "), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("corrupt config must not fail load: %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %+v, want defaults", got)
	}
	if _, err := os.Stat(p.Config + ".bak"); err != nil {
		t.Fatalf("expected corrupt config to be backed up: %v", err)
	}
}
