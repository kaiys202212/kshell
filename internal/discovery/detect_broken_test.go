package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

// 只剩配置目录的 provider：Tool 必须透传 Broken，供 UI 与自动修复使用。
type brokenFakeProvider struct{ home string }

func (p *brokenFakeProvider) ID() string          { return "fake" }
func (p *brokenFakeProvider) DisplayName() string { return "Fake" }
func (p *brokenFakeProvider) DetectSpec(string) providers.DetectSpec {
	return providers.DetectSpec{
		BinName:     "no-such-bin-xyz",
		InstallDirs: []string{filepath.Join(p.home, "nowhere")},
		ConfigDirs:  []string{"~/.fake-tool"},
	}
}
func (p *brokenFakeProvider) SessionRoots(string) []string { return nil }
func (p *brokenFakeProvider) SessionFilePattern() string   { return "*.jsonl" }
func (p *brokenFakeProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, nil
}
func (p *brokenFakeProvider) NewSessionCmd(ws, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: ws}
}
func (p *brokenFakeProvider) ResumeCmd(s providers.Session, bin string) providers.Launch {
	return providers.Launch{Path: bin, Dir: s.Workspace}
}

func TestDetectAllPassesBrokenThrough(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".fake-tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	tools := DetectAll(home, []providers.Provider{&brokenFakeProvider{home: home}})
	if len(tools) != 1 {
		t.Fatalf("len=%d, want 1", len(tools))
	}
	if !tools[0].Installed || !tools[0].Broken {
		t.Fatalf("Installed=%v Broken=%v, want true/true", tools[0].Installed, tools[0].Broken)
	}
	if tools[0].Version != "unknown" {
		t.Fatalf("Version=%q（无 BinPath 不应探测）", tools[0].Version)
	}
}
