package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestOpenCodeSyncCreatesProviderNamespace(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// 预置用户/CC Switch 块，同步后必须保留
	seed := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"other": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Other",
				"options": map[string]any{
					"baseURL": "https://other.example/v1",
					"apiKey":  "other-key",
				},
				"models": map[string]any{"m1": map[string]any{"name": "m1"}},
			},
		},
		"mcp": map[string]any{"keep": true},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "智谱", OpenAIBaseURL: "https://api.zhipu.example/v1",
			APIKey: "sk-z", Models: []string{"glm-x", "glm-y"},
		}},
	}
	if err := (OpenCode{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	doc := readOpenCodeJSON(t, home)
	prov, _ := doc["provider"].(map[string]any)
	if _, ok := prov["other"]; !ok {
		t.Fatal("应保留用户 provider.other")
	}
	if doc["mcp"] == nil {
		t.Fatal("应保留 mcp")
	}
	ks, ok := prov["kshell-p1"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 kshell-p1: %#v", prov)
	}
	if ks["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("npm = %v", ks["npm"])
	}
	if ks["name"] != "智谱" {
		t.Fatalf("name = %v", ks["name"])
	}
	opts, _ := ks["options"].(map[string]any)
	if opts["baseURL"] != "https://api.zhipu.example/v1" || opts["apiKey"] != "sk-z" {
		t.Fatalf("options = %#v", opts)
	}
	models, _ := ks["models"].(map[string]any)
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
}

func TestOpenCodeSyncRemovesStaleAndSkipsNoOpenAI(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"provider": map[string]any{
			"kshell-old": map[string]any{"name": "old"},
			"keep":       map[string]any{"name": "keep"},
		},
	}
	raw, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	in := SyncInput{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "ant-only", Name: "仅 Anthropic", AnthropicBaseURL: "https://ant.example", APIKey: "k", Models: []string{"m"}},
			{ID: "p2", Name: "P2", OpenAIBaseURL: "https://oai.example/v1", APIKey: "k2", Models: []string{"m2"}},
		},
	}
	if err := (OpenCode{}).Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readOpenCodeJSON(t, home)
	prov, _ := doc["provider"].(map[string]any)
	if _, ok := prov["kshell-old"]; ok {
		t.Fatal("应删除过期 kshell-old")
	}
	if _, ok := prov["kshell-ant-only"]; ok {
		t.Fatal("无 OpenAI URL 不应写入")
	}
	if _, ok := prov["keep"]; !ok {
		t.Fatal("应保留 keep")
	}
	if _, ok := prov["kshell-p2"]; !ok {
		t.Fatal("应写入 kshell-p2")
	}
}

func TestOpenCodeSyncDisabledClearsNamespace(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"provider": map[string]any{
			"kshell-p1": map[string]any{"name": "p1"},
			"user":      map[string]any{"name": "user"},
		},
	}
	raw, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (OpenCode{}).Sync(home, SyncInput{Enabled: false}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readOpenCodeJSON(t, home)
	prov, _ := doc["provider"].(map[string]any)
	if _, ok := prov["kshell-p1"]; ok {
		t.Fatal("Enabled=false 应清 kshell-*")
	}
	if _, ok := prov["user"]; !ok {
		t.Fatal("应保留 user")
	}
}

func TestOpenCodeConfigExists(t *testing.T) {
	home := t.TempDir()
	oc := OpenCode{}
	if oc.ConfigExists(home) {
		t.Fatal("无文件时 ConfigExists 应为 false")
	}
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !oc.ConfigExists(home) {
		t.Fatal("有 opencode.json 时 ConfigExists 应为 true")
	}
}

func readOpenCodeJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
