package discovery

import "testing"

func TestFormatParseSSHRefRoundTrip(t *testing.T) {
	ref := FormatSSHRef("c1", "/home/u/proj")
	if ref != "ssh://c1/home/u/proj" {
		t.Fatalf("got %q", ref)
	}
	kind, id, path, err := ParseWorkspaceRef(ref)
	if err != nil || kind != KindSSH || id != "c1" || path != "/home/u/proj" {
		t.Fatalf("parse: %s %s %s %v", kind, id, path, err)
	}
}

func TestParseWorkspaceRefLocal(t *testing.T) {
	kind, id, path, err := ParseWorkspaceRef(`D:\code\foo`)
	if err != nil || kind != KindLocal || id != "" || path == "" {
		t.Fatalf("local parse failed: %v", err)
	}
}

func TestNormalizeRemotePath(t *testing.T) {
	if NormalizeRemotePath("/home/u/../u/proj//") != "/home/u/proj" {
		t.Fatal("normalize")
	}
}
