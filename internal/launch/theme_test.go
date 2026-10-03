package launch

import (
	"reflect"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func TestForSessionInjectsGenericAndProviderTheme(t *testing.T) {
	ps := []providers.Provider{providers.Claude{}}
	tools := []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude.exe"}}
	s := providers.Session{ID: "s1", ToolID: "claude", Workspace: `D:\ws`}

	l, err := ForSession(ps, tools, s, ThemeOptions{Mode: appearance.Light}, ModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["COLORFGBG"] != "0;15" || l.Env["COLORTERM"] != "truecolor" {
		t.Fatalf("env = %v", l.Env)
	}
	want := []string{"--resume", "s1", "--settings", `{"theme":"light"}`}
	if !reflect.DeepEqual(l.Args, want) {
		t.Fatalf("args = %v, want %v", l.Args, want)
	}
}

func TestForWorkspaceToolGenericOnlyForNonThemer(t *testing.T) {
	ps := []providers.Provider{providers.Generic{Spec: providers.GenericSpec{ID: "gtool"}}}
	tools := []discovery.Tool{{ID: "gtool", Installed: true, BinPath: "gtool.exe"}}
	ws := discovery.Workspace{Path: `D:\ws`}

	l, err := ForWorkspaceTool(ps, tools, ws, "gtool", ThemeOptions{Mode: appearance.Dark}, ModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["COLORFGBG"] != "15;0" {
		t.Fatalf("env = %v", l.Env)
	}
	if len(l.Args) != 0 {
		t.Fatalf("args = %v, want empty", l.Args)
	}
}

func TestForSessionGeminiWritesFileEnv(t *testing.T) {
	dir := t.TempDir()
	ps := []providers.Provider{providers.Gemini{}}
	tools := []discovery.Tool{{ID: "gemini", Installed: true, BinPath: "gemini.exe"}}
	s := providers.Session{ID: "g1", ToolID: "gemini", Workspace: `D:\ws`}

	l, err := ForSession(ps, tools, s, ThemeOptions{Mode: appearance.Dark, CacheDir: dir}, ModelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] == "" {
		t.Fatalf("env = %v", l.Env)
	}
}
