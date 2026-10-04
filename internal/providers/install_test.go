package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)


func TestBuiltinsImplementInstaller(t *testing.T) {
	for _, p := range Builtins() {
		if _, ok := RecipeOf(p); !ok {
			t.Fatalf("%s 必须实现 Installer", p.ID())
		}
	}
}

func TestGenericHasNoInstaller(t *testing.T) {
	g := Generic{Spec: GenericSpec{ID: "cline", Name: "Cline"}}
	if _, ok := RecipeOf(g); ok {
		t.Fatal("自定义工具不得实现 Installer")
	}
}

func TestClaudeRecipeUsesNpm(t *testing.T) {
	r, ok := RecipeOf(Claude{})
	if !ok {
		t.Fatal("missing recipe")
	}
	if !strings.Contains(r.InstallCmd, "@anthropic-ai/claude-code") {
		t.Fatalf("install = %q", r.InstallCmd)
	}
	if !strings.Contains(r.UninstallCmd, "uninstall") {
		t.Fatalf("uninstall = %q", r.UninstallCmd)
	}
	if len(r.PurgeDirs) != 1 || r.PurgeDirs[0] != "~/.claude" {
		t.Fatalf("purge = %v", r.PurgeDirs)
	}
}

func TestCursorRecipeForbidsPurge(t *testing.T) {
	r, ok := RecipeOf(Cursor{})
	if !ok {
		t.Fatal("missing recipe")
	}
	if len(r.PurgeDirs) != 0 {
		t.Fatalf("cursor 禁止 PurgeDirs, got %v", r.PurgeDirs)
	}
	if r.UninstallCmd != "" {
		t.Fatalf("cursor 卸载应走删二进制, UninstallCmd=%q", r.UninstallCmd)
	}
	if runtime.GOOS == "windows" {
		if r.Shell != "powershell" {
			t.Fatalf("windows cursor shell = %q", r.Shell)
		}
		const want = `irm 'https://cursor.com/install?win32=true' | iex`
		if r.InstallCmd != want {
			t.Fatalf("windows cursor InstallCmd = %q, want %q", r.InstallCmd, want)
		}
	}
}

func TestNpmToolsRecipes(t *testing.T) {
	cases := []struct {
		p      Provider
		pkg    string
		purges []string
	}{
		{Codex{}, "@openai/codex", []string{"~/.codex"}},
		{Gemini{}, "@google/gemini-cli", []string{"~/.gemini"}},
		{Opencode{}, "opencode-ai", []string{"~/.config/opencode", "~/.local/share/opencode"}},
	}
	for _, c := range cases {
		r, ok := RecipeOf(c.p)
		if !ok {
			t.Fatalf("%s missing", c.p.ID())
		}
		if !strings.Contains(r.InstallCmd, c.pkg) || !strings.Contains(r.UninstallCmd, c.pkg) {
			t.Fatalf("%s cmds %q %q", c.p.ID(), r.InstallCmd, r.UninstallCmd)
		}
		if len(r.PurgeDirs) != len(c.purges) {
			t.Fatalf("%s purge %v", c.p.ID(), r.PurgeDirs)
		}
		for i, d := range c.purges {
			if r.PurgeDirs[i] != d {
				t.Fatalf("%s purge[%d]=%s", c.p.ID(), i, r.PurgeDirs[i])
			}
		}
	}
}

// 卸载要删掉每一份拷贝：根目录同时有旧名与新名 shim 时，FindBins 都要列出来。
func TestFindBinsCoversAltNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 才有多后缀候选")
	}
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	dir := filepath.Join(local, "cursor-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldBin := writeFakeBin(t, dir, "cursor-agent", "echo cursor-agent")
	newBin := writeFakeBin(t, dir, "agent", "echo agent")

	got := FindBins(Cursor{}.DetectSpec(home), home)
	if len(got) != 2 {
		t.Fatalf("FindBins = %v, want 旧名与新名两份", got)
	}
	for _, p := range got {
		if filepath.Clean(p) != filepath.Clean(oldBin) && filepath.Clean(p) != filepath.Clean(newBin) {
			t.Fatalf("意外路径 %q", p)
		}
	}
}
