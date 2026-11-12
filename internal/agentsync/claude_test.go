package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	in := SyncInput{Enabled: true, ClaudePlatformID: "p1", Platforms: []config.ModelPlatform{
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
	in := SyncInput{Enabled: true, ClaudePlatformID: "p1", Platforms: []config.ModelPlatform{
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
	if err := c.Sync(home, SyncInput{Enabled: false, Platforms: []config.ModelPlatform{{ID: "p1"}}}); err != nil {
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
	in := SyncInput{Enabled: true, ClaudePlatformID: "p1", Platforms: []config.ModelPlatform{
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

// ClaudePlatformID 缺省回落第一个平台（与 config.ActivePlatform 语义一致）。
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
	in1.ClaudePlatformID = "p1"
	if err := c.Sync(home, in1); err != nil {
		t.Fatal(err)
	}
	in2 := base
	in2.Enabled = true
	in2.ClaudePlatformID = "p2"
	if err := c.Sync(home, in2); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "https://b.example/" || env["ANTHROPIC_AUTH_TOKEN"] != "kb" {
		t.Fatalf("切平台后 env=%v", env)
	}
}

// 4 个槽位显式映射到对应 env 键（不再从 ActiveModel/平台列表隐式推导）。
func TestClaudeSyncWritesSlotModels(t *testing.T) {
	home := t.TempDir()
	c := Claude{}
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk-1",
			Models: []string{"glm-5.3-flash", "deepseek-v4.1-flash", "kimi-k3"},
		}},
		ToolSync: config.ToolSyncConfig{Claude: config.ClaudeSync{
			Model: "glm-5.3-flash", Sonnet: "glm-5.3-flash",
			Opus: "deepseek-v4.1-flash", Haiku: "kimi-k3",
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

// 槽位留空 = 不写对应键（先删后建语义下清掉残留）；槽位值带空白时 trim。
func TestClaudeSyncEmptySlotsNotWrittenAndTrim(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_MODEL":"stale","ANTHROPIC_DEFAULT_OPUS_MODEL":"stale-opus"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{ID: "p1", AnthropicBaseURL: "https://ant.example/"}},
		ToolSync:  config.ToolSyncConfig{Claude: config.ClaudeSync{Model: "  glm-5.3-flash  "}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("MODEL 槽应 trim 后写入: %v", env["ANTHROPIC_MODEL"])
	}
	for _, k := range []string{"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"} {
		if _, ok := env[k]; ok {
			t.Fatalf("空槽不应写 %s（残留应被清掉）: %v", k, env)
		}
	}
}

// 从未配置过模型（无任何 kshell 痕迹）：settings.json 必须原样保留——用户或
// ccswitch 写入的端点/模型/密钥都不能被清；但仍补写登录标记（只动登录）。
func TestClaudeSyncUnmanagedLeavesSettingsUntouched(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://ccswitch.example/","ANTHROPIC_AUTH_TOKEN":"sk-user","ANTHROPIC_MODEL":"user-model"},"model":"sonnet"}`)
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (Claude{}).Sync(home, SyncInput{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(original) {
		t.Fatalf("未配置过模型时不得改写 settings.json:\n got=%s\nwant=%s", b, original)
	}
	ob, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("应仍补写登录标记: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(ob, &doc); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if doc["hasCompletedOnboarding"] != true {
		t.Fatalf("hasCompletedOnboarding = %v", doc["hasCompletedOnboarding"])
	}
}

// 未配置过模型且没有 settings.json：连文件都不该创建（只写登录标记）。
func TestClaudeSyncUnmanagedDoesNotCreateSettings(t *testing.T) {
	home := t.TempDir()
	if err := (Claude{}).Sync(home, SyncInput{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("不应创建 settings.json: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("应写入登录标记: %v", err)
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
	if err := (Claude{}).Sync(home, SyncInput{Enabled: false, Platforms: []config.ModelPlatform{{ID: "p1"}}}); err != nil {
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
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk",
			Models: []string{"glm-5.3-flash", "deepseek-v4.1-flash"},
		}},
		ToolSync: config.ToolSyncConfig{Claude: config.ClaudeSync{Model: "glm-5.3-flash"}},
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
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/",
			Models: []string{"glm-5.3-flash"},
		}},
		ToolSync: config.ToolSyncConfig{Claude: config.ClaudeSync{Model: "glm-5.3-flash"}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeSettings(t, home)["model"]; got != "glm-5.3-flash" {
		t.Fatalf("槽位内 model 应保留, got %v", got)
	}
}

// 无任何槽位配置：不校验顶层 model（自由输入）。
func TestClaudeSyncKeepsTopLevelModelWhenNoPlatformList(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"free-form"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/",
		}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeSettings(t, home)["model"]; got != "free-form" {
		t.Fatalf("无槽位时应保留顶层 model, got %v", got)
	}
}

// 仅 Opus 槽命中（其余槽未配）时仍保留顶层 model。
func TestClaudeSyncKeepsTopLevelModelMatchingActiveOnly(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"active-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/",
			Models: []string{"listed-a", "listed-b"},
		}},
		ToolSync: config.ToolSyncConfig{Claude: config.ClaudeSync{Opus: "active-only"}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	if got := readClaudeSettings(t, home)["model"]; got != "active-only" {
		t.Fatalf("槽位命中应保留, got %v", got)
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
		Enabled: true, ClaudePlatformID: "p1",
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

// claudeDisplayKeys 是纳入清理的显示名/残留键组(4 别名 × NAME/DESCRIPTION/
// SUPPORTED_CAPABILITIES + FABLE 别名 ID 键),与 claude.go 的 kshellEnvKeys 对应。
var claudeDisplayKeys = []string{
	"ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_NAME",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_NAME",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES",
}

func seedDisplayKeys(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"KEEP": "yes"}
	for _, k := range claudeDisplayKeys {
		env[k] = "MiniMax-M3"
	}
	b, err := json.Marshal(map[string]any{"env": env})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Enabled 同步:残留显示名键被清除(实际请求走 _MODEL ID 键,但 /model label
// 优先取 _NAME,残留旧平台显示名会让用户误以为模型没切换),且绝不代写。
func TestClaudeSyncRemovesStaleDisplayKeys(t *testing.T) {
	home := t.TempDir()
	seedDisplayKeys(t, home)
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant.example/", APIKey: "sk",
			Models: []string{"gpt-5", "deepseek-v4.1-flash", "glm-5.3"},
		}},
		ToolSync: config.ToolSyncConfig{Claude: config.ClaudeSync{Model: "glm-5.3-flash"}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	for _, k := range claudeDisplayKeys {
		if _, ok := env[k]; ok {
			t.Fatalf("%s 应被清除: %v", k, env)
		}
	}
	if env["KEEP"] != "yes" {
		t.Fatalf("无关用户键应保留: %v", env)
	}
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("现有管理键写入不受影响: %v", env["ANTHROPIC_MODEL"])
	}
}

// Disabled 同步同样清理显示名键(与 6 管理键的先删语义一致)。
func TestClaudeSyncDisabledRemovesDisplayKeys(t *testing.T) {
	home := t.TempDir()
	seedDisplayKeys(t, home)
	if err := (Claude{}).Sync(home, SyncInput{Enabled: false, Platforms: []config.ModelPlatform{{ID: "p1"}}}); err != nil {
		t.Fatal(err)
	}
	env := readClaudeSettings(t, home)["env"].(map[string]any)
	for _, k := range claudeDisplayKeys {
		if _, ok := env[k]; ok {
			t.Fatalf("disabled 时 %s 也应被清除: %v", k, env)
		}
	}
	if env["KEEP"] != "yes" {
		t.Fatalf("无关用户键应保留: %v", env)
	}
}

// Sync 无条件把 ~/.claude.json 的 hasCompletedOnboarding 置 true,跳过新版
// Claude Code 首启的登录/引导页(触发条件选 B:与注入 Enabled/平台无关)。
func TestClaudeSyncWritesOnboardingSkip(t *testing.T) {
	onboardingPath := func(home string) string {
		return filepath.Join(home, ".claude.json")
	}
	in := SyncInput{Enabled: false}

	t.Run("missing 新建", func(t *testing.T) {
		home := t.TempDir()
		if err := (Claude{}).Sync(home, in); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		b, err := os.ReadFile(onboardingPath(home))
		if err != nil {
			t.Fatalf("应新建 ~/.claude.json: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("非法 JSON: %v", err)
		}
		if doc["hasCompletedOnboarding"] != true {
			t.Fatalf("hasCompletedOnboarding = %v", doc["hasCompletedOnboarding"])
		}
	})

	t.Run("merge 保留既有内容与大整型", func(t *testing.T) {
		home := t.TempDir()
		original := `{"projects":{"d:/w":{"history":[]}},"firstStartTime":1700000000000}`
		if err := os.WriteFile(onboardingPath(home), []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := (Claude{}).Sync(home, in); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		b, err := os.ReadFile(onboardingPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `1700000000000`) {
			t.Fatalf("大整型被重写变形: %s", b)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("非法 JSON: %v", err)
		}
		if doc["hasCompletedOnboarding"] != true {
			t.Fatalf("hasCompletedOnboarding = %v", doc["hasCompletedOnboarding"])
		}
		if _, ok := doc["projects"]; !ok {
			t.Fatalf("既有顶层键被破坏: %v", doc)
		}
	})

	t.Run("noop 已是 true 不改文件", func(t *testing.T) {
		home := t.TempDir()
		original := []byte(`{"hasCompletedOnboarding":true,"theme":"dark"}`)
		if err := os.WriteFile(onboardingPath(home), original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := (Claude{}).Sync(home, in); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		b, err := os.ReadFile(onboardingPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != string(original) {
			t.Fatalf("已完成引导时文件被重写: %s", b)
		}
	})

	t.Run("corrupt 损坏文件跳过不写", func(t *testing.T) {
		home := t.TempDir()
		original := `{"hasCompletedOnboarding":`
		if err := os.WriteFile(onboardingPath(home), []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := (Claude{}).Sync(home, in); err != nil {
			t.Fatalf("损坏文件不应让 Sync 报错: %v", err)
		}
		b, err := os.ReadFile(onboardingPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != original {
			t.Fatalf("损坏文件被改动: %s", b)
		}
	})
}

// 默认平台只有 OpenAI 协议(无 AnthropicBaseURL)时,kshell 不接管 Claude 的
// 端点/模型:陈旧模型键被清掉,平台模型列表不得写成 ANTHROPIC_MODEL,顶层
// model(官方登录用户自选)也不得被平台列表校验误删。
func TestClaudeSyncSkipsModelKeysWithoutAnthropicEndpoint(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"my-choice","env":{"ANTHROPIC_MODEL":"stale","KEEP":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SyncInput{
		Enabled: true, ClaudePlatformID: "p1", ActiveModel: "gpt-5",
		Platforms: []config.ModelPlatform{{
			ID: "p1", OpenAIBaseURL: "https://oai.example/v1", APIKey: "sk-oai",
			Models: []string{"gpt-5", "deepseek-v4.1-flash"},
		}},
	}
	if err := (Claude{}).Sync(home, in); err != nil {
		t.Fatal(err)
	}
	doc := readClaudeSettings(t, home)
	env := doc["env"].(map[string]any)
	for _, k := range []string{
		"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
	} {
		if _, ok := env[k]; ok {
			t.Fatalf("OpenAI-only 平台不应写 %s: %v", k, env)
		}
	}
	if env["KEEP"] != "1" {
		t.Fatalf("无关用户键应保留: %v", env)
	}
	if doc["model"] != "my-choice" {
		t.Fatalf("不接管时顶层 model 应保留, got %v", doc["model"])
	}
}
