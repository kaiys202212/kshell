package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func readClaudeSettings(t *testing.T, home string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("读 settings.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("settings.json 非法 JSON: %v", err)
	}
	return doc
}

// 全新目录：Enabled + 默认平台 Anthropic 端点 → 写入两键，并自动建目录。
func TestClaudeSyncFreshWrite(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{Enabled: true, DefaultPlatformID: "p1", Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk-1"},
		{ID: "p2", AnthropicBaseURL: "https://other.example/", APIKey: "sk-2"},
	}}
	if err := c.Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://ant.example/" {
		t.Fatalf("ANTHROPIC_BASE_URL = %v", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-1" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %v", env["ANTHROPIC_AUTH_TOKEN"])
	}
}

// 键级合并：用户 settings.json 既有内容（其他 env 键、顶层键）必须保留。
func TestClaudeSyncPreservesUserContent(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"env":{"KEEP":"yes"},"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Claude{}
	in := SyncInput{Enabled: true, DefaultPlatformID: "p1", Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk-1"},
	}}
	if err := c.Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readClaudeSettings(t, home)
	if doc["theme"] != "dark" {
		t.Fatalf("顶层键被破坏: %v", doc)
	}
	env := doc["env"].(map[string]any)
	if env["KEEP"] != "yes" {
		t.Fatalf("用户 env 键被破坏: %v", env)
	}
}

// Enabled=false：删除 kshell 管理键，其余保留；env 空了则不残留空对象。
func TestClaudeSyncDisabledRemovesKeys(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_BASE_URL":"https://old/","ANTHROPIC_AUTH_TOKEN":"old","OTHER":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Claude{}
	if err := c.Sync(home, SyncInput{Enabled: false}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("ANTHROPIC_BASE_URL 应被清除: %v", env)
	}
	if _, ok := env["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN 应被清除: %v", env)
	}
	if env["OTHER"] != "x" {
		t.Fatalf("用户其他键应保留: %v", env)
	}
}

// 默认平台无 Anthropic 端点：不写 BASE_URL；APIKey 也无从谈起。
func TestClaudeSyncNoAnthropicURLWritesNothing(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{Enabled: true, DefaultPlatformID: "p1", Platforms: []config.ModelPlatform{
		{ID: "p1", OpenAIBaseURL: "https://oai.example/", APIKey: "sk-1"},
	}}
	if err := c.Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	doc := readClaudeSettings(t, home)
	if _, ok := doc["env"]; ok {
		t.Fatalf("无 Anthropic 端点不应写 env: %v", doc)
	}
}

// DefaultPlatformID 缺省回落第一个平台（与 config.ActivePlatform 语义一致）。
func TestClaudeSyncDefaultsToFirstPlatform(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{Enabled: true, Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://first.example/"},
		{ID: "p2", AnthropicBaseURL: "https://second.example/"},
	}}
	if err := c.Sync(home, in); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://first.example/" {
		t.Fatalf("应回落第一个平台: %v", env["ANTHROPIC_BASE_URL"])
	}
}

// 换默认平台：旧键被覆盖为新平台值（ccswitch「切平台」语义）。
func TestClaudeSyncSwitchesPlatform(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	base := SyncInput{Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://a.example/", APIKey: "ka"},
		{ID: "p2", AnthropicBaseURL: "https://b.example/", APIKey: "kb"},
	}}
	in1 := base
	in1.Enabled = true
	in1.DefaultPlatformID = "p1"
	if err := c.Sync(home, in1); err != nil {
		t.Fatal(err)
	}
	in2 := base
	in2.Enabled = true
	in2.DefaultPlatformID = "p2"
	if err := c.Sync(home, in2); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://b.example/" || env["ANTHROPIC_AUTH_TOKEN"] != "kb" {
		t.Fatalf("切平台后 env=%v", env)
	}
}

// ActiveModel 写入 ANTHROPIC_MODEL，并把平台模型列表映射到 Sonnet/Opus/Haiku，
// 否则 Claude TUI /model 只能看到 settings.json 里陈旧的 mimo 等值。
func TestClaudeSyncWritesActiveModelAndDefaults(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{
		Enabled: true, DefaultPlatformID: "p1", ActiveModel: "glm-5.3-flash",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk-1",
			Models: []string{"glm-5.3-flash", "deepseek-v4.1-flash", "kimi-k3", "glm-5.3"},
		}},
	}
	if err := c.Sync(home, in); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("ANTHROPIC_MODEL = %v", env["ANTHROPIC_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("SONNET = %v", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "deepseek-v4.1-flash" {
		t.Fatalf("OPUS = %v", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "kimi-k3" {
		t.Fatalf("HAIKU = %v", env["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	}
}

// ActiveModel 空时回落平台模型列表首项；无模型则不写 MODEL 键。
func TestClaudeSyncModelFallsBackToFirstPlatformModel(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{Enabled: true, DefaultPlatformID: "p1", Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://ant.example/", Models: []string{"kimi-k3"}},
	}}
	if err := c.Sync(home, in); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "kimi-k3" {
		t.Fatalf("应回落首项: %v", env["ANTHROPIC_MODEL"])
	}

	home2 := t.TempDir()
	in2 := SyncInput{Enabled: true, DefaultPlatformID: "p1", Platforms: []config.ModelPlatform{
		{ID: "p1", AnthropicBaseURL: "https://ant.example/"},
	}}
	if err := c.Sync(home2, in2); err != nil {
		t.Fatal(err)
	}
	env2 := readClaudeSettings(t, home2)["env"].(map[string]any)
	for _, k := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"} {
		if _, ok := env2[k]; ok {
			t.Fatalf("无模型名不应写 %s: %v", k, env2)
		}
	}
}

// Enabled=false 时清除模型键（含用户 settings 里残留的 mimo 等）。
func TestClaudeSyncDisabledRemovesModelKeys(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_MODEL":"mimo-v2.5-pro","ANTHROPIC_DEFAULT_SONNET_MODEL":"mimo-v2.5","KEEP":"yes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Claude{}).Sync(home, SyncInput{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_MODEL"]; ok {
		t.Fatalf("ANTHROPIC_MODEL 应被清除: %v", env)
	}
	if env["KEEP"] != "yes" {
		t.Fatalf("其他键应保留: %v", env)
	}
}

// 顶层 model 指向已不在默认平台列表中的旧模型 → 删除，避免 /model Custom 悬空。
func TestClaudeSyncClearsStaleTopLevelModel(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"gone-mid","env":{"KEEP":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, DefaultPlatformID: "p1", ActiveModel: "glm-5.3-flash",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk",
			Models: []string{"glm-5.3-flash", "deepseek-v4.1-flash"},
		}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	doc := readClaudeSettings(t, home)
	if _, ok := doc["model"]; ok {
		t.Fatalf("悬空顶层 model 应被清除: %v", doc["model"])
	}
	env := doc["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("ANTHROPIC_MODEL = %v", env["ANTHROPIC_MODEL"])
	}
}

func TestClaudeSyncKeepsTopLevelModelWhenInPlatformList(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"glm-5.3-flash"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, DefaultPlatformID: "p1", ActiveModel: "glm-5.3-flash",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/",
			Models: []string{"glm-5.3-flash"},
		}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeSettings(t, home)["model"]; got != "glm-5.3-flash" {
		t.Fatalf("列表内 model 应保留, got %v", got)
	}
}

func TestClaudeSyncKeepsBuiltinAliasTopLevelModel(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"sonnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, DefaultPlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/",
			Models: []string{"glm-5.3-flash"},
		}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeSettings(t, home)["model"]; got != "sonnet" {
		t.Fatalf("内置别名应保留, got %v", got)
	}
}

// Claude 注册进 DefaultTargets 且 ToolID 正确，SyncAll/SyncTool 才能路由到它。
func TestClaudeInDefaultTargets(t *testing.T) {
	found := false
	for _, tgt := range DefaultTargets() {
		if tgt.ToolID() == "claude" {
			found = true
		}
	}
	if !found {
		t.Fatal("DefaultTargets 应包含 claude")
	}
}
