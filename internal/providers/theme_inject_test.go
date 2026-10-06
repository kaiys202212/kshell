package providers

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/yangk/kshell/internal/appearance"
)

func TestClaudeThemeOverrides(t *testing.T) {
	args, env := Claude{}.ThemeOverrides(appearance.ThemeDark, "")
	if env != nil {
		t.Fatalf("env = %v, want nil", env)
	}
	if len(args) != 2 || args[0] != "--settings" || args[1] != `{"theme":"dark"}` {
		t.Fatalf("args = %v", args)
	}
	args, _ = Claude{}.ThemeOverrides(appearance.ThemeLight, "")
	if args[1] != `{"theme":"light"}` {
		t.Fatalf("args = %v", args)
	}
}

func TestCodexThemeOverrides(t *testing.T) {
	dark, _ := Codex{}.ThemeOverrides(appearance.ThemeDark, "")
	if len(dark) != 2 || dark[0] != "-c" || dark[1] != "tui.theme=catppuccin-mocha" {
		t.Fatalf("dark args = %v", dark)
	}
	light, _ := Codex{}.ThemeOverrides(appearance.ThemeLight, "")
	if light[1] != "tui.theme=catppuccin-latte" {
		t.Fatalf("light args = %v", light)
	}
}

func TestGeminiWritesSystemSettings(t *testing.T) {
	dir := t.TempDir()
	_, env := Gemini{}.ThemeOverrides(appearance.ThemeLight, dir)
	path := env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"]
	if path == "" {
		t.Fatal("missing GEMINI_CLI_SYSTEM_SETTINGS_PATH")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		UI struct {
			Theme string `json:"theme"`
			Auto  bool   `json:"autoThemeSwitching"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.UI.Theme != "Default Light" || got.UI.Auto {
		t.Fatalf("ui = %+v", got.UI)
	}
}

func TestOpencodeWritesTUIConfig(t *testing.T) {
	dir := t.TempDir()
	_, env := Opencode{}.ThemeOverrides(appearance.ThemeDark, dir)
	path := env["OPENCODE_TUI_CONFIG"]
	if path == "" {
		t.Fatal("missing OPENCODE_TUI_CONFIG")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 不用 system：system 会 OSC 查色，应答经 xterm onData 回写后污染首条消息/标题
	if string(data) != `{"theme":"opencode"}` {
		t.Fatalf("dark data = %s", data)
	}
	_, env = Opencode{}.ThemeOverrides(appearance.ThemeLight, dir)
	data, err = os.ReadFile(env["OPENCODE_TUI_CONFIG"])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"theme":"catppuccin-latte"}` {
		t.Fatalf("light data = %s", data)
	}
}

func TestFileInjectSkippedWithoutCacheDir(t *testing.T) {
	if _, env := (Gemini{}).ThemeOverrides(appearance.ThemeDark, ""); env != nil {
		t.Fatalf("gemini env = %v", env)
	}
	if _, env := (Opencode{}).ThemeOverrides(appearance.ThemeDark, ""); env != nil {
		t.Fatalf("opencode env = %v", env)
	}
}

func TestEnvListSortedAndNil(t *testing.T) {
	got := EnvList(map[string]string{"B": "2", "A": "1"})
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("EnvList = %v", got)
	}
	if EnvList(nil) != nil {
		t.Fatal("EnvList(nil) should be nil")
	}
}

func TestMergeEnvOverrides(t *testing.T) {
	got := MergeEnv(map[string]string{"A": "1", "B": "1"}, map[string]string{"B": "2", "C": "3"})
	if got["A"] != "1" || got["B"] != "2" || got["C"] != "3" {
		t.Fatalf("MergeEnv = %v", got)
	}
	if MergeEnv(nil, nil) != nil {
		t.Fatal("MergeEnv(nil,nil) should be nil")
	}
}
