package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func newModelApp(t *testing.T) (*App, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	return NewAppWith(Options{Config: config.Default(), Layout: layout}), layout
}

func TestGetModelConfigNeverLeaksKey(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{APIKey: "secret", Agents: map[string]string{"claude": "m"}}
	app.mu.Unlock()

	v, err := app.GetModelConfig()
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if !v.APIKeySet {
		t.Fatal("APIKeySet 应为 true")
	}
	if v.Agents["claude"] != "m" {
		t.Fatalf("agents = %+v", v.Agents)
	}
}

func TestSetModelConfigKeepOverwriteClear(t *testing.T) {
	app, layout := newModelApp(t)

	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true, Preset: "deepseek",
		OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/",
		APIKey: "k1",
		Agents: map[string]string{"claude": " m ", "codex": "gpt-x"},
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	loaded, _ := config.Load(layout)
	if loaded.Model.APIKey != "k1" || !loaded.Model.Enabled || loaded.Model.Preset != "deepseek" {
		t.Fatalf("persist 1 = %+v", loaded.Model)
	}
	if loaded.Model.OpenAIBaseURL != "https://oai/" || loaded.Model.AnthropicBaseURL != "https://ant/" {
		t.Fatalf("urls = %+v", loaded.Model)
	}
	if loaded.Model.Agents["claude"] != "m" {
		t.Fatalf("agents 未 TrimSpace: %q", loaded.Model.Agents["claude"])
	}

	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true, OpenAIBaseURL: "https://oai/", Agents: map[string]string{},
	}); err != nil {
		t.Fatalf("Set keep: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.APIKey != "k1" {
		t.Fatalf("留空应保持密钥，got %q", loaded.Model.APIKey)
	}

	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, ClearAPIKey: true, Agents: map[string]string{}}); err != nil {
		t.Fatalf("Set clear: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.APIKey != "" {
		t.Fatalf("ClearAPIKey 应清空，got %q", loaded.Model.APIKey)
	}

	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, OpenAIBaseURL: "ftp://bad"}); err == nil {
		t.Fatal("非法 OpenAI BaseURL 应报错")
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.OpenAIBaseURL == "ftp://bad" {
		t.Fatalf("非法 URL 不应落盘，got %q", loaded.Model.OpenAIBaseURL)
	}
}

func TestListModelPresetsNonEmpty(t *testing.T) {
	app, _ := newModelApp(t)
	list := app.ListModelPresets()
	if len(list) < 3 {
		t.Fatalf("预设过少: %d", len(list))
	}
}

func TestSessionAndPermissionModeRoundTrip(t *testing.T) {
	app, layout := newModelApp(t)
	if app.GetSessionMode() != config.SessionModeTUI {
		t.Fatalf("default session = %q", app.GetSessionMode())
	}
	if err := app.SetSessionMode(config.SessionModeACP); err != nil {
		t.Fatal(err)
	}
	if err := app.SetPermissionMode(config.PermissionModeBypass); err != nil {
		t.Fatal(err)
	}
	loaded, _ := config.Load(layout)
	if loaded.SessionMode != config.SessionModeACP || loaded.PermissionMode != config.PermissionModeBypass {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := app.SetSessionMode("bogus"); err == nil {
		t.Fatal("非法 session_mode 应报错")
	}
}
