package desktop

import (
	"os"
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
	app.opts.Config.Model = config.ModelConfig{APIKey: "secret", Models: []string{"m"}, ActiveModel: "m"}
	app.mu.Unlock()

	v, err := app.GetModelConfig()
	if err != nil {
		t.Fatalf("GetModelConfig: %v", err)
	}
	if !v.APIKeySet {
		t.Fatal("APIKeySet 应为 true")
	}
	if len(v.Models) != 1 || v.Models[0] != "m" || v.ActiveModel != "m" {
		t.Fatalf("models = %+v", v)
	}
}

func TestSetModelConfigKeepOverwriteClear(t *testing.T) {
	app, layout := newModelApp(t)

	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true, Preset: "deepseek",
		OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/",
		APIKey:      "k1",
		Models:      []string{" m ", "gpt-x", "m", ""},
		ActiveModel: " gpt-x ",
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
	if len(loaded.Model.Models) != 2 || loaded.Model.Models[0] != "m" || loaded.Model.Models[1] != "gpt-x" {
		t.Fatalf("models 应 TrimSpace/去空/去重: %#v", loaded.Model.Models)
	}
	if loaded.Model.ActiveModel != "gpt-x" {
		t.Fatalf("active_model 未 TrimSpace: %q", loaded.Model.ActiveModel)
	}

	if err := app.SetModelConfig(ModelConfigInput{
		Enabled: true, OpenAIBaseURL: "https://oai/", Models: []string{},
	}); err != nil {
		t.Fatalf("Set keep: %v", err)
	}
	loaded, _ = config.Load(layout)
	if loaded.Model.APIKey != "k1" {
		t.Fatalf("留空应保持密钥，got %q", loaded.Model.APIKey)
	}

	if err := app.SetModelConfig(ModelConfigInput{Enabled: true, ClearAPIKey: true, Models: []string{}}); err != nil {
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

func TestModelOptionsUsesActiveModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled: true,
		OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/",
		APIKey: "k", ActiveModel: "glm-5",
	}
	app.mu.Unlock()

	mo := app.modelOptions()
	for _, toolID := range []string{"claude", "codex", "anything"} {
		mc, ok := mo.Resolver(toolID)
		if !ok || mc.Model != "glm-5" || mc.OpenAIBaseURL != "https://oai/" || mc.APIKey != "k" {
			t.Fatalf("resolver(%s) = %+v ok=%v", toolID, mc, ok)
		}
	}
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{Enabled: false, ActiveModel: "glm-5"}
	app.mu.Unlock()
	if mo := app.modelOptions(); mo.Resolver != nil {
		t.Fatal("未启用时不应注入")
	}
}

// writeCodebuddySettings 在 t.TempDir 下造一个 ~/.codebuddy/settings.json。
func writeCodebuddySettings(t *testing.T, model string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"model":"` + model + `"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCodebuddyLastModel(t *testing.T) {
	// 有记录：返回该值
	home := writeCodebuddySettings(t, "custom-local:deepseek-v4.1-flash")
	if got := codebuddyLastModel(home); got != "custom-local:deepseek-v4.1-flash" {
		t.Fatalf("codebuddyLastModel = %q", got)
	}
	// 无 settings.json：空串
	if got := codebuddyLastModel(t.TempDir()); got != "" {
		t.Fatalf("missing file: got %q", got)
	}
	// model 为空：空串
	home = writeCodebuddySettings(t, "")
	if got := codebuddyLastModel(home); got != "" {
		t.Fatalf("empty model: got %q", got)
	}
	// JSON 非法：空串
	home = t.TempDir()
	dir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codebuddyLastModel(home); got != "" {
		t.Fatalf("bad json: got %q", got)
	}
}

func TestModelOptionsCodebuddyPrefersLastModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Home = writeCodebuddySettings(t, "custom-local:deepseek-v4.1-flash")
	app.opts.Config.Model = config.ModelConfig{Enabled: true, ActiveModel: "glm-5"}
	app.mu.Unlock()

	mo := app.modelOptions()
	// codebuddy：沿用记住的上次选择
	mc, ok := mo.Resolver("codebuddy")
	if !ok || mc.Model != "custom-local:deepseek-v4.1-flash" {
		t.Fatalf("resolver(codebuddy) = %+v ok=%v", mc, ok)
	}
	// 其他工具：仍用 ActiveModel
	mc, ok = mo.Resolver("claude")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(claude) = %+v ok=%v", mc, ok)
	}
	// codebuddy 无记录：回落 ActiveModel
	app.mu.Lock()
	app.opts.Home = t.TempDir()
	app.mu.Unlock()
	mc, ok = mo.Resolver("codebuddy")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(codebuddy, no record) = %+v ok=%v", mc, ok)
	}
}

// writeCursorConfig 在 t.TempDir 下造一个 ~/.cursor/cli-config.json。
func writeCursorConfig(t *testing.T, modelID string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"selectedModel":{"modelId":"` + modelID + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCursorLastModel(t *testing.T) {
	// 有记录：返回该值
	home := writeCursorConfig(t, "grok-4.6")
	if got := cursorLastModel(home); got != "grok-4.6" {
		t.Fatalf("cursorLastModel = %q", got)
	}
	// "default"（Auto）：返回空串（回落 ActiveModel）
	home = writeCursorConfig(t, "default")
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("default model: got %q", got)
	}
	// modelId 为空：空串
	home = writeCursorConfig(t, "")
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("empty modelId: got %q", got)
	}
	// 无 cli-config.json：空串
	if got := cursorLastModel(t.TempDir()); got != "" {
		t.Fatalf("missing file: got %q", got)
	}
	// JSON 非法：空串
	home = t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cursorLastModel(home); got != "" {
		t.Fatalf("bad json: got %q", got)
	}
}

func TestModelOptionsCursorPrefersLastModel(t *testing.T) {
	app, _ := newModelApp(t)
	app.mu.Lock()
	app.opts.Home = writeCursorConfig(t, "grok-4.7")
	app.opts.Config.Model = config.ModelConfig{Enabled: true, ActiveModel: "glm-5"}
	app.mu.Unlock()

	mo := app.modelOptions()
	// cursor：沿用记住的上次选择
	mc, ok := mo.Resolver("cursor")
	if !ok || mc.Model != "grok-4.7" {
		t.Fatalf("resolver(cursor) = %+v ok=%v", mc, ok)
	}
	// 其他工具：仍用 ActiveModel
	mc, ok = mo.Resolver("codebuddy")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(codebuddy) = %+v ok=%v", mc, ok)
	}
	// cursor 记录为 default（Auto）：回落 ActiveModel
	app.mu.Lock()
	app.opts.Home = writeCursorConfig(t, "default")
	app.mu.Unlock()
	mc, ok = mo.Resolver("cursor")
	if !ok || mc.Model != "glm-5" {
		t.Fatalf("resolver(cursor, default) = %+v ok=%v", mc, ok)
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
