package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWriteAndManifest(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}
	files := []File{{
		Path: "SKILL.md",
		Contents: "---\nname: demo-skill\ndescription: for tests\n---\n# Demo\n",
	}}
	dir, name, err := s.WriteEntity("acme/repo/demo-skill", files)
	if err != nil {
		t.Fatal(err)
	}
	if name != "demo-skill" {
		t.Fatalf("name %q", name)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	rec := InstalledSkill{
		ID:         "acme/repo/demo-skill",
		Name:       name,
		Source:     "acme/repo",
		EntityPath: RelEntityPath(root, dir),
		Targets:    map[string]TargetRecord{"claude": {Mode: modeLink, Path: "/tmp/x"}},
	}
	if err := s.UpsertInstalled(rec); err != nil {
		t.Fatal(err)
	}
	m, err := s.ReadManifest()
	if err != nil || m.Skills["acme/repo/demo-skill"].Name != "demo-skill" {
		t.Fatalf("%+v %v", m, err)
	}
	if err := s.RemoveInstalled("acme/repo/demo-skill", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("entity should be gone: %v", err)
	}
}
