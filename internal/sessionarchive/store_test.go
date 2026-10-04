package sessionarchive

import (
	"path/filepath"
	"testing"
)

func TestStorePersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "archived.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("s1"); err != nil {
		t.Fatal(err)
	}
	if !s.Has("s1") || len(s.IDs()) != 1 {
		t.Fatalf("ids = %#v", s.IDs())
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Has("s1") {
		t.Fatal("reopen lost s1")
	}
	if err := s2.Remove("s1"); err != nil {
		t.Fatal(err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s3.Has("s1") {
		t.Fatal("remove did not persist")
	}
}
