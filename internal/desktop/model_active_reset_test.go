package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// 保存空平台列表时必须丢弃 ActiveModel：否则配置里会留下「kshell 托管过模型」
// 的唯一痕迹，之后每次启动同步都会清掉 ccswitch 写入的 ANTHROPIC_*。
// 本次保存仍应按旧配置做一次性清理。
func TestSetModelConfigDropsActiveModelWithoutPlatforms(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	layout := config.Layout{
		Root: dir, Config: filepath.Join(dir, "config.yaml"), Cache: filepath.Join(dir, "cache"),
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settings,
		[]byte(`{"env":{"ANTHROPIC_BASE_URL":"https://old/","ANTHROPIC_AUTH_TOKEN":"k","KEEP":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Model = config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://old/", APIKey: "k",
		}},
	}
	app := NewAppWith(Options{Home: home, Config: cfg, Layout: layout})
	app.mu.Lock()
	app.tools = []discovery.Tool{{ID: "claude", Installed: true}}
	app.mu.Unlock()

	// 前端可能仍带着旧 ActiveModel 一起提交空平台列表。
	if err := app.SetModelConfig(ModelConfigInput{Enabled: false, ActiveModel: "m1"}); err != nil {
		t.Fatalf("SetModelConfig: %v", err)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Model.ActiveModel != "" {
		t.Fatalf("空平台列表不得保留 ActiveModel，got %q", loaded.Model.ActiveModel)
	}

	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	env, _ := doc["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("删光平台后应清掉 kshell 注入键: %v", env)
	}
	if env["KEEP"] != "1" {
		t.Fatalf("用户其他 env 键应保留: %v", env)
	}
}
