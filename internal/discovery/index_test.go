package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

func writeSessionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeFixtureContent(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "providers", "testdata", "claude", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

func TestScanCollectsSessionsAndWorkspaces(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "projects", "D--data-workspace-demo")
	writeSessionFile(t, filepath.Join(sessionsDir, "sess-1.jsonl"), claudeFixtureContent(t))

	cachePath := filepath.Join(t.TempDir(), "cache", "index.json")
	res, err := Scan(home, []providers.Provider{providers.Claude{}}, cachePath, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(res.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1: %+v", len(res.Sessions), res.Sessions)
	}
	if res.Sessions[0].ID != "42a6304b-1fd3-45aa-b620-10aa37988f2a" {
		t.Fatalf("session id = %q", res.Sessions[0].ID)
	}
	if len(res.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1: %+v", len(res.Workspaces), res.Workspaces)
	}
	if res.Workspaces[0].Path != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", res.Workspaces[0].Path)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %v", res.Failed)
	}
}

func TestScanCountsParseFailures(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "projects", "D--data-workspace-demo")
	writeSessionFile(t, filepath.Join(sessionsDir, "broken.jsonl"), "not json at all\n")

	res, err := Scan(home, []providers.Provider{providers.Claude{}}, filepath.Join(t.TempDir(), "index.json"), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("failed = %v, want 1 entry", res.Failed)
	}
	if len(res.Sessions) != 0 {
		t.Fatalf("sessions = %+v, want none", res.Sessions)
	}
}

func TestScanIgnoresMissingProviderDirs(t *testing.T) {
	res, err := Scan(t.TempDir(), []providers.Provider{providers.Claude{}, providers.Codex{}}, filepath.Join(t.TempDir(), "index.json"), ScanOptions{})
	if err != nil {
		t.Fatalf("Scan on empty home must not fail: %v", err)
	}
	if len(res.Sessions) != 0 || len(res.Workspaces) != 0 {
		t.Fatalf("got %d sessions / %d workspaces, want none", len(res.Sessions), len(res.Workspaces))
	}
}

func TestScanRespectsGlobDepth(t *testing.T) {
	// CodeBuddy：subagents/ 下的深层 jsonl 是子代理记录，不得当成独立会话收录
	home := t.TempDir()
	proj := filepath.Join(home, ".codebuddy", "projects", "d-ws-demo")
	// 与真实 CodeBuddy 结构一致：subagents 挂在 <sessionId>\（不带 .jsonl 后缀）目录下
	writeSessionFile(t, filepath.Join(proj, "main-1", "subagents", "agent-1.jsonl"),
		`{"type":"session-meta","sessionId":"agent-1","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}`+"\n")
	writeSessionFile(t, filepath.Join(proj, "main-1.jsonl"),
		`{"type":"session-meta","sessionId":"cb-1","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}`+"\n"+
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"检查会话识别"}]}`+"\n")

	cachePath := filepath.Join(t.TempDir(), "index.json")
	res, err := Scan(home, []providers.Provider{genericCodebuddyProvider(t)}, cachePath, ScanOptions{})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(res.Sessions) != 1 || res.Sessions[0].ID != "cb-1" {
		t.Fatalf("sessions = %+v, want only main session cb-1", res.Sessions)
	}
}

func genericCodebuddyProvider(t *testing.T) providers.Provider {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.yaml")
	yaml := `
providers:
  - id: codebuddy
    name: CodeBuddy
    sessions:
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	specs, err := providers.LoadGenericSpecs(path)
	if err != nil || len(specs) != 1 {
		t.Fatalf("LoadGenericSpecs: %v, %d specs", err, len(specs))
	}
	return providers.Generic{Spec: specs[0], Home: ""}
}

func TestScanWritesCacheAndReusesIt(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "projects", "D--data-workspace-demo")
	sessionPath := filepath.Join(sessionsDir, "sess-1.jsonl")
	writeSessionFile(t, sessionPath, claudeFixtureContent(t))

	cachePath := filepath.Join(t.TempDir(), "cache", "index.json")
	if _, err := Scan(home, []providers.Provider{providers.Claude{}}, cachePath, ScanOptions{}); err != nil {
		t.Fatal(err)
	}

	idx := LoadIndex(cachePath)
	if len(idx.Entries) != 1 {
		t.Fatalf("cache entries = %d, want 1", len(idx.Entries))
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.shouldReuse(sessionPath, info.ModTime().UnixNano(), info.Size()) {
		t.Fatal("unchanged file should hit the cache")
	}
	if idx.shouldReuse(sessionPath, info.ModTime().UnixNano()+1, info.Size()) {
		t.Fatal("changed mtime must invalidate the cache")
	}
	if idx.shouldReuse(sessionPath, info.ModTime().UnixNano(), info.Size()+1) {
		t.Fatal("changed size must invalidate the cache")
	}
}

func TestScanMergesGitWorkspacesWithoutDuplicates(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "projects", "D--data-workspace-demo")
	writeSessionFile(t, filepath.Join(sessionsDir, "sess-1.jsonl"), claudeFixtureContent(t))

	// 一个已被会话覆盖的仓库 + 一个全新的仓库
	scanRoot := t.TempDir()
	mkdirAll(t, filepath.Join(scanRoot, "known", ".git"))
	mkdirAll(t, filepath.Join(scanRoot, "fresh", ".git"))

	opts := ScanOptions{Roots: []string{scanRoot}, MaxDepth: 4, Exclude: []string{".git", "node_modules"}}
	res, err := Scan(home, []providers.Provider{providers.Claude{}}, filepath.Join(t.TempDir(), "index.json"), opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	// 会话工作区 (D:\data\workspace\demo) + fresh 仓库；known 与会话目录不同名，也会被追加
	if len(res.Workspaces) != 3 {
		t.Fatalf("workspaces = %d, want 3: %+v", len(res.Workspaces), res.Workspaces)
	}
	gitCount := 0
	for _, w := range res.Workspaces {
		if w.Source == "git" {
			gitCount++
		}
	}
	if gitCount != 2 {
		t.Fatalf("git workspaces = %d, want 2: %+v", gitCount, res.Workspaces)
	}
}

func TestScanWithoutRootsSkipsGitScan(t *testing.T) {
	res, err := Scan(t.TempDir(), []providers.Provider{providers.Claude{}}, filepath.Join(t.TempDir(), "index.json"), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Workspaces) != 0 {
		t.Fatalf("no roots should mean no git scan, got %+v", res.Workspaces)
	}
}

func TestLoadIndexRecoversFromCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx := LoadIndex(path)
	if idx.Entries == nil {
		t.Fatal("corrupt index should fall back to an empty index")
	}
}
