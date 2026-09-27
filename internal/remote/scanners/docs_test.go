package scanners

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsScannerFindsHost(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ssh", "id_ed25519"), []byte("KEY MATERIAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, dir, "README.md", "部署：ssh deploy@server.internal 上去 restart 即可\n联系 a@b.com\n")

	got, err := DocsScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	if got[0].Host != "server.internal" || got[0].User != "deploy" {
		t.Fatalf("candidate = %+v", got[0])
	}
	if got[0].Confidence != "low" {
		t.Fatalf("confidence = %q, want low", got[0].Confidence)
	}
	if !strings.HasSuffix(got[0].IdentityFile, "id_ed25519") {
		t.Fatalf("identity should point at the workspace key, got %q", got[0].IdentityFile)
	}
}

func TestDocsScannerNeverReadsKeyContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, ".ssh", "id_rsa")
	if err := os.WriteFile(keyPath, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, dir, "CLAUDE.md", "服务器 ops@box.internal\n")

	got, err := DocsScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	for _, field := range []string{got[0].IdentityFile, got[0].Name, got[0].Host} {
		if strings.Contains(field, "PRIVATE KEY") {
			t.Fatalf("key material leaked into a candidate: %q", field)
		}
	}
}

func TestDocsScannerMatch(t *testing.T) {
	s := DocsScanner{}
	for _, name := range []string{"README.md", "readme.md", "CLAUDE.md", "AGENTS.md"} {
		if !s.Match(name) {
			t.Fatalf("%s should match", name)
		}
	}
	if s.Match("docs/design.md") {
		t.Fatal("only top-level docs should match")
	}
}
