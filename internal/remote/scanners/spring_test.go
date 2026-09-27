package scanners

import (
	"testing"
)

const ymlSample = `
server:
  port: 8080
deploy:
  ssh:
    host: 10.0.0.9
    user: ops
    port: 2201
    keyPath: ~/.ssh/id_rsa
spring:
  datasource:
    url: jdbc:mysql://db.internal:3306/app
    username: app
`

func TestSpringScannerYaml(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "application.yml", ymlSample)

	got, err := SpringScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Host != "10.0.0.9" || got[0].User != "ops" || got[0].Port != 2201 {
		t.Fatalf("candidate = %+v", got[0])
	}
	if got[0].Confidence != "medium" || got[0].Source != "spring" {
		t.Fatalf("confidence/source = %q/%q", got[0].Confidence, got[0].Source)
	}
}

func TestSpringScannerIgnoresDatasource(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "application.yml", "spring:\n  datasource:\n    host: db.internal\n    username: app\n    port: 3306\n")

	got, err := SpringScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("datasource must not be reported as ssh: %+v", got)
	}
}

func TestSpringScannerProperties(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "application.properties", "deploy.ssh.host=1.2.3.4\ndeploy.ssh.user=root\nserver.port=8080\n")

	got, err := SpringScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Host != "1.2.3.4" || got[0].User != "root" {
		t.Fatalf("got %+v", got)
	}
}

func TestSpringScannerSkipsLoneHost(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "application.yml", "misc:\n  host: example.com\n")

	got, err := SpringScanner{}.Extract(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("host without user/port/key should not be a candidate: %+v", got)
	}
}

func TestSpringScannerMatch(t *testing.T) {
	s := SpringScanner{}
	for _, name := range []string{"application.yml", "application-prod.yaml", "application.properties"} {
		if !s.Match(name) {
			t.Fatalf("%s should match", name)
		}
	}
	if s.Match("bootstrap.yml") || s.Match("main.go") {
		t.Fatal("only application* configs should match")
	}
}
