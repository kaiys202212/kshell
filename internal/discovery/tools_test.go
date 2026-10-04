package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

type stubProvider struct {
	id   string
	spec providers.DetectSpec
}

func (s stubProvider) ID() string                             { return s.id }
func (s stubProvider) DisplayName() string                    { return s.id }
func (s stubProvider) DetectSpec(string) providers.DetectSpec { return s.spec }
func (s stubProvider) SessionRoots(string) []string           { return nil }
func (s stubProvider) SessionFilePattern() string             { return "*.jsonl" }
func (s stubProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, nil
}
func (s stubProvider) NewSessionCmd(string, string) providers.Launch {
	return providers.Launch{}
}
func (s stubProvider) ResumeCmd(providers.Session, string) providers.Launch {
	return providers.Launch{}
}

func writeFakeBin(t *testing.T, dir, name string) {
	t.Helper()
	var path string
	var body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".cmd")
		body = "@echo off\r\necho " + name + " 1.2.3\r\n"
	} else {
		path = filepath.Join(dir, name)
		body = "#!/bin/sh\necho " + name + " 1.2.3\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
}

func TestDetectAllPutsInstalledFirst(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	tools := DetectAll(t.TempDir(), []providers.Provider{
		stubProvider{id: "zulu"},
		stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}},
		stubProvider{id: "alpha"},
	})

	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(tools))
	}
	if tools[0].ID != "claude" || !tools[0].Installed {
		t.Fatalf("installed tool should come first, got %+v", tools[0])
	}
	if tools[1].ID != "alpha" || tools[2].ID != "zulu" {
		t.Fatalf("uninstalled tools should be sorted by id, got %s, %s", tools[1].ID, tools[2].ID)
	}
	if tools[0].Version != "claude 1.2.3" {
		t.Fatalf("version = %q, want %q", tools[0].Version, "claude 1.2.3")
	}
}

func TestDetectAllKeepsUninstalledTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tools := DetectAll(t.TempDir(), []providers.Provider{stubProvider{id: "gemini"}})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if tools[0].Installed {
		t.Fatalf("missing tool must still be reported as uninstalled: %+v", tools[0])
	}
	if tools[0].Version != "unknown" {
		t.Fatalf("version = %q, want unknown", tools[0].Version)
	}
}

func TestDetectAll_FillsACP(t *testing.T) {
	tools := DetectAll("", []providers.Provider{providers.Claude{}})
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].ACP == nil {
		t.Fatal("ACP detection not attached")
	}
}

func TestDetectAllCarriesBinArgs(t *testing.T) {
	home := t.TempDir()
	vdir := filepath.Join(home, "va")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 精确命名的 node 入口文件（writeFakeBin 会改后缀，不能用）
	if err := os.WriteFile(filepath.Join(vdir, "node.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "index.js"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := stubProvider{id: "cursor", spec: providers.DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{vdir},
		NodeEntryScript: "index.js",
	}}
	probe := func(bin string, args []string) string {
		if len(args) != 1 || args[0] != "index.js" {
			t.Fatalf("probe 应收到 BinArgs, got %v", args)
		}
		return "2026.10.01"
	}
	tools := detectAll(home, []providers.Provider{p}, probe)
	if !tools[0].Installed || tools[0].BinPath == "" {
		t.Fatalf("应检出 node 入口, got %+v", tools[0])
	}
	if len(tools[0].BinArgs) != 1 || tools[0].BinArgs[0] != "index.js" {
		t.Fatalf("Tool 应携带 BinArgs, got %+v", tools[0])
	}
	if tools[0].Version != "2026.10.01" {
		t.Fatalf("version = %q", tools[0].Version)
	}
}
