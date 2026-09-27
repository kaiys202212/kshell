package scanners

import (
	"strings"
	"testing"
)

const sampleConfig = `
# 注释
Host *
    ServerAliveInterval 60

Host prod
    HostName prod.example.com
    User deploy
    Port 2222
    IdentityFile ~/.ssh/id_ed25519

Host staging
    HostName 10.0.0.5
    User ubuntu
`

func TestParseSSHConfigHosts(t *testing.T) {
	got := ParseSSHConfig(strings.NewReader(sampleConfig))
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 (wildcard excluded): %+v", len(got), got)
	}

	prod := got[0]
	if prod.Name != "prod" || prod.Host != "prod.example.com" || prod.User != "deploy" || prod.Port != 2222 {
		t.Fatalf("prod parsed wrong: %+v", prod)
	}
	if !strings.Contains(prod.IdentityFile, "id_ed25519") {
		t.Fatalf("identity file missing: %+v", prod)
	}
	if prod.Confidence != "high" {
		t.Fatalf("confidence = %q, want high", prod.Confidence)
	}

	staging := got[1]
	if staging.Host != "10.0.0.5" || staging.User != "ubuntu" {
		t.Fatalf("staging parsed wrong: %+v", staging)
	}
	if staging.Port != 22 {
		t.Fatalf("default port = %d, want 22", staging.Port)
	}
}

func TestParseSSHConfigUsesHostWhenNoHostName(t *testing.T) {
	got := ParseSSHConfig(strings.NewReader("Host myserver\n    User root\n"))
	if len(got) != 1 || got[0].Host != "myserver" {
		t.Fatalf("got %+v, want host fall back to the Host value", got)
	}
}

func TestParseSSHConfigMultipleHostsOnOneLine(t *testing.T) {
	got := ParseSSHConfig(strings.NewReader("Host a b\n    User root\n"))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("names = %s, %s", got[0].Name, got[1].Name)
	}
}

func TestSSHConfigScannerMatch(t *testing.T) {
	s := SSHConfigScanner{}
	if !s.Match(".ssh/config") {
		t.Fatal(".ssh/config should match")
	}
	if !s.Match("ssh_config") {
		t.Fatal("ssh_config should match")
	}
	if s.Match("src/main.go") {
		t.Fatal("unrelated files must not match")
	}
}

func TestSSHConfigScannerExtract(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".ssh/config", sampleConfig)

	got, err := SSHConfigScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Source != "sshconfig" {
		t.Fatalf("source = %q", got[0].Source)
	}
	if got[0].SourceFile != path {
		t.Fatalf("source file = %q, want %q", got[0].SourceFile, path)
	}
}
