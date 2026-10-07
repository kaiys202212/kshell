package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceInstallUninstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[{"path":"SKILL.md","contents":"---\nname: demo\ndescription: d\n---\nbody"}]}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	svc := NewService(home)
	svc.Client = &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	// 模拟已检测 claude
	targets := svc.ListTargets([]string{"claude", "unknown"})
	if len(targets) != 1 || !targets[0].DefaultChecked {
		t.Fatalf("%+v", targets)
	}

	res, err := svc.Install(context.Background(), "acme/repo/demo", []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "demo" || len(res.Targets) != 1 {
		t.Fatalf("%+v", res)
	}
	entity := filepath.Join(home, ".kshell", "skills", "acme", "repo", "demo", "SKILL.md")
	if _, err := os.Stat(entity); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(home, ".claude", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if err := svc.Uninstall("acme/repo/demo", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent target still there: %v", err)
	}
}
