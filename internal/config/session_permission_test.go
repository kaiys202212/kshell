package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionModeDefaultIsTUI(t *testing.T) {
	if got := Default().SessionMode; got != SessionModeTUI {
		t.Fatalf("default session_mode = %q, want %q", got, SessionModeTUI)
	}
}

func TestPermissionModeDefaultIsDefault(t *testing.T) {
	if got := Default().PermissionMode; got != PermissionModeDefault {
		t.Fatalf("default permission_mode = %q, want %q", got, PermissionModeDefault)
	}
}

func TestSessionPermissionNormalized(t *testing.T) {
	cases := []struct {
		session, perm, wantSession, wantPerm string
	}{
		{"", "", SessionModeTUI, PermissionModeDefault},
		{"tui", "default", SessionModeTUI, PermissionModeDefault},
		{"acp", "bypass", SessionModeACP, PermissionModeBypass},
		{"ACP", "BYPASS", SessionModeTUI, PermissionModeDefault},
		{"bogus", "bogus", SessionModeTUI, PermissionModeDefault},
	}
	for _, c := range cases {
		cfg := Config{MaxDepth: 1, SessionMode: c.session, PermissionMode: c.perm}
		got := cfg.normalized()
		if got.SessionMode != c.wantSession || got.PermissionMode != c.wantPerm {
			t.Fatalf("in session=%q perm=%q → got %q/%q, want %q/%q",
				c.session, c.perm, got.SessionMode, got.PermissionMode, c.wantSession, c.wantPerm)
		}
	}
}

func TestLegacyBaseURLMigratesToDual(t *testing.T) {
	dir := t.TempDir()
	p := Layout{Root: dir, Config: filepath.Join(dir, "config.yaml"), Cache: filepath.Join(dir, "cache")}
	raw := "model:\n  enabled: true\n  base_url: https://legacy.example/v1\n  api_key: k\n"
	if err := os.WriteFile(p.Config, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.OpenAIBaseURL != "https://legacy.example/v1" {
		t.Fatalf("openai = %q", got.Model.OpenAIBaseURL)
	}
	if got.Model.AnthropicBaseURL != "https://legacy.example/v1" {
		t.Fatalf("anthropic = %q", got.Model.AnthropicBaseURL)
	}
}

func TestModelDualURLRoundTrip(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	cfg.Model.Enabled = true
	cfg.Model.Preset = "deepseek"
	cfg.Model.OpenAIBaseURL = "https://api.deepseek.com"
	cfg.Model.AnthropicBaseURL = "https://api.deepseek.com/anthropic"
	cfg.Model.Agents["claude"] = "deepseek-chat"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.Preset != "deepseek" ||
		loaded.Model.OpenAIBaseURL != cfg.Model.OpenAIBaseURL ||
		loaded.Model.AnthropicBaseURL != cfg.Model.AnthropicBaseURL {
		t.Fatalf("loaded model = %+v", loaded.Model)
	}
}

func TestModelSaveOmitsEmptyLegacyBaseURL(t *testing.T) {
	p := tempPaths(t)
	cfg := Default()
	cfg.Model.OpenAIBaseURL = "https://a.example"
	cfg.Model.AnthropicBaseURL = "https://b.example"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "base_url:" || strings.HasPrefix(strings.TrimSpace(line), "base_url:") {
			t.Fatalf("不应写出空 legacy base_url:\n%s", data)
		}
	}
}
