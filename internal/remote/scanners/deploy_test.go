package scanners

import (
	"testing"
)

func TestDeployScannerDockerHost(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", "services:\n  app:\n    environment:\n      DOCKER_HOST=ssh://root@1.2.3.4:2222\n")

	got, err := DeployScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	if got[0].Host != "1.2.3.4" || got[0].User != "root" || got[0].Port != 2222 {
		t.Fatalf("candidate = %+v", got[0])
	}
	if got[0].Confidence != "low" {
		t.Fatalf("confidence = %q, want low", got[0].Confidence)
	}
}

func TestDeployScannerSSHCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "deploy.sh", "#!/bin/sh\nssh -p 2200 ops@10.0.0.3 \"docker compose up -d\"\n")

	got, err := DeployScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	if got[0].Host != "10.0.0.3" || got[0].User != "ops" || got[0].Port != 2200 {
		t.Fatalf("candidate = %+v", got[0])
	}
}

func TestDeployScannerAnsibleInventory(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "inventory", "web ansible_host=192.168.1.5 ansible_user=centos ansible_port=2222\n")

	got, err := DeployScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	if got[0].Host != "192.168.1.5" || got[0].User != "centos" || got[0].Port != 2222 {
		t.Fatalf("candidate = %+v", got[0])
	}
}

func TestDeployScannerDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "deploy.sh", "ssh ops@10.0.0.3 up\nssh ops@10.0.0.3 down\n")

	got, err := DeployScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("duplicate hosts should collapse, got %+v", got)
	}
}

func TestDeployScannerMatch(t *testing.T) {
	s := DeployScanner{}
	for _, name := range []string{"docker-compose.yml", "Makefile", "inventory", "deploy.sh", "package.json"} {
		if !s.Match(name) {
			t.Fatalf("%s should match", name)
		}
	}
	if s.Match("src/main.go") {
		t.Fatal("unrelated files must not match")
	}
}
