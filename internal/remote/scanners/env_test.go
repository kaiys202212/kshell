package scanners

import (
	"testing"
)

func TestEnvScanner(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "SSH_HOST=10.0.0.7\nSSH_USER=ops\nSSH_PORT=2222\nSSH_KEY=~/.ssh/id_rsa\nOTHER=ignored\n")

	got, err := EnvScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}

	c := got[0]
	if c.Host != "10.0.0.7" || c.User != "ops" || c.Port != 2222 {
		t.Fatalf("candidate = %+v", c)
	}
	if c.IdentityFile == "" || c.IdentityFile == "~/.ssh/id_rsa" {
		t.Fatalf("identity should be expanded to an absolute path, got %q", c.IdentityFile)
	}
	if c.Confidence != "medium" || c.Source != "env" {
		t.Fatalf("confidence/source = %q/%q", c.Confidence, c.Source)
	}
}

func TestEnvScannerTargetForm(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env.prod", "SSH_TARGET=root@prod.example.com\n")

	got, err := EnvScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Host != "prod.example.com" || got[0].User != "root" {
		t.Fatalf("got %+v, want root@prod.example.com", got)
	}
}

func TestEnvScannerWithoutHost(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "SSH_USER=ops\nSSH_PORT=22\n")

	got, err := EnvScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("no host means no candidate, got %+v", got)
	}
}

func TestEnvScannerMatch(t *testing.T) {
	s := EnvScanner{}
	if !s.Match(".env") || !s.Match(".env.production") {
		t.Fatal(".env files should match")
	}
	if s.Match("src/main.go") {
		t.Fatal("unrelated files must not match")
	}
}
