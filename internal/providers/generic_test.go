package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const genericYAML = `
providers:
  - id: codebuddy
    name: CodeBuddy
    detect:
      command: codebuddy
      dirs:
        - ~/.codebuddy
    sessions:
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: message.content
    resume:
      args: ["--resume", "{id}"]
    verified: false
`

func TestLoadGenericSpecs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(path, []byte(genericYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := LoadGenericSpecs(path)
	if err != nil {
		t.Fatalf("LoadGenericSpecs error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %d, want 1", len(specs))
	}
	if specs[0].ID != "codebuddy" || specs[0].Sessions.Glob != "~/.codebuddy/projects/*/*.jsonl" {
		t.Fatalf("spec = %+v", specs[0])
	}
	if specs[0].Verified {
		t.Fatal("preset must be marked unverified")
	}
}

func TestGenericProviderRootsAndPattern(t *testing.T) {
	home := t.TempDir()
	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: home}

	roots := g.SessionRoots(home)
	want := filepath.Join(home, ".codebuddy", "projects")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %v, want [%s]", roots, want)
	}
	if got := g.SessionFilePattern(); got != "*.jsonl" {
		t.Fatalf("pattern = %q", got)
	}
}

func TestGenericProviderParsesFixture(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "projects", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess.jsonl")
	content := `{"type":"user","cwd":"D:\\data\\workspace\\demo","sessionId":"cb-1","timestamp":"2026-09-20T10:00:00Z","message":{"content":"看一下这个工程"}}` + "\n" +
		`{"type":"assistant","cwd":"D:\\data\\workspace\\demo","sessionId":"cb-1","timestamp":"2026-09-20T10:05:00Z","message":{"content":"好的"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: home}

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "cb-1" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "看一下这个工程" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Messages != 2 {
		t.Fatalf("messages = %d, want 2", got.Messages)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("timestamps: %v ~ %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGenericResumeCmdSubstitutesID(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec}

	launch := g.ResumeCmd(Session{ID: "cb-1", Workspace: "D:\\ws"}, "codebuddy")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "cb-1" {
		t.Fatalf("args = %v, want [--resume cb-1]", launch.Args)
	}
	if launch.Dir != "D:\\ws" {
		t.Fatalf("dir = %q", launch.Dir)
	}
}

func TestDefaultProvidersYAMLCoversPresetTools(t *testing.T) {
	content := DefaultProvidersYAML()
	for _, want := range []string{"codebuddy", "opencode", "cline", "verified: false"} {
		if !strings.Contains(content, want) {
			t.Fatalf("default providers.yaml missing %q", want)
		}
	}
}

func TestEnsureProvidersFileWritesTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "providers.yaml")
	if err := EnsureProvidersFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "providers:") {
		t.Fatalf("template not written: %s", data)
	}

	// 已存在时不应覆盖
	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProvidersFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "custom" {
		t.Fatalf("existing file must not be overwritten, got %s", data)
	}
}

func TestRootFromGlob(t *testing.T) {
	if got := rootFromGlob("~/.a/*/x.jsonl"); got != "~/.a" {
		t.Fatalf("got %q", got)
	}
	if got := rootFromGlob("~/.a/x.jsonl"); got != "~/.a/x.jsonl" {
		t.Fatalf("got %q", got)
	}
}
