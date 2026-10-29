package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func writeSettings(t *testing.T, home string, doc map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSettingsDoc(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func readSettingsEnv(t *testing.T, home string) map[string]any {
	t.Helper()
	env, _ := readSettingsDoc(t, home)["env"].(map[string]any)
	return env
}

func claudeOptions(home string, mc config.ModelConfig) Options {
	return Options{Home: home, Config: config.Config{MaxDepth: 10, Model: mc}}
}

// stubDetect 替换工具探测，避免单测真跑本机 CLI 的版本探测。
func stubDetect(t *testing.T, tools []discovery.Tool) {
	t.Helper()
	prev := detectTools
	detectTools = func(string, []providers.Provider) []discovery.Tool { return tools }
	t.Cleanup(func() { detectTools = prev })
}

// Anthropic 端点启用时，平台模型写入 settings.json 的 env（含三个 DEFAULT 别名）。
func TestSyncAgentModelsWritesClaudeSettings(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{})
	mc := config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		ActiveModel:     "m-active",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "智谱",
			AnthropicBaseURL: "https://ant/", APIKey: "k1",
			Models: []string{"m1", "m2", "m3"},
		}},
	}

	syncAgentModels(home, mc, nil)

	env := readSettingsEnv(t, home)
	// ActiveModel 不在平台列表里：Sonnet 用 ActiveModel，Opus/Haiku 顺延列表前两项。
	want := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://ant/",
		"ANTHROPIC_AUTH_TOKEN":           "k1",
		"ANTHROPIC_MODEL":                "m-active",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "m-active",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "m1",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "m2",
	}
	for k, v := range want {
		if got, _ := env[k].(string); got != v {
			t.Fatalf("env[%s] = %q, want %q（env=%v）", k, got, v, env)
		}
	}
}

// 仅配 OpenAI 协议时不接管 Claude：陈旧 ANTHROPIC_* 被清理，用户其他键保留。
func TestSyncAgentModelsSkipsClaudeWithoutAnthropicEndpoint(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"env": map[string]any{
		"ANTHROPIC_BASE_URL": "https://stale/",
		"KEEP":               "1",
	}})
	mc := config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", OpenAIBaseURL: "https://oai/", APIKey: "k1",
			Models: []string{"gpt-x"},
		}},
	}

	syncAgentModels(home, mc, nil)

	env := readSettingsEnv(t, home)
	if env["KEEP"] != "1" {
		t.Fatalf("用户 env 键应保留: %v", env)
	}
	for k := range env {
		if strings.HasPrefix(k, "ANTHROPIC_") {
			t.Fatalf("仅 OpenAI 平台不得保留 %s: %v", k, env)
		}
	}
}

// 禁用平台时清掉陈旧的 kshell 管理键；env 清空后整个键删除，其他键保留。
func TestSyncAgentModelsClearsWhenDisabled(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"env": map[string]any{
		"ANTHROPIC_BASE_URL": "https://stale/",
		"ANTHROPIC_MODEL":    "stale",
		"KEEP":               "1",
	}})
	mc := config.ModelConfig{
		Enabled:   false,
		Platforms: []config.ModelPlatform{{ID: "p1"}},
	}

	syncAgentModels(home, mc, nil)

	env := readSettingsEnv(t, home)
	if env["KEEP"] != "1" {
		t.Fatalf("用户 env 键应保留: %v", env)
	}
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL"} {
		if _, ok := env[k]; ok {
			t.Fatalf("禁用后应清掉 %s: %v", k, env)
		}
	}
}

// env 只剩 kshell 管理键时，清理后整个 env 键应删除。
func TestSyncAgentModelsRemovesEmptyEnv(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"env": map[string]any{
		"ANTHROPIC_BASE_URL": "https://stale/",
	}})

	syncAgentModels(home, config.ModelConfig{
		Enabled:   false,
		Platforms: []config.ModelPlatform{{ID: "p1"}},
	}, nil)

	if _, ok := readSettingsDoc(t, home)["env"]; ok {
		t.Fatalf("清空后不应保留 env 键: %v", readSettingsDoc(t, home))
	}
}

// 从未配置过平台模型（默认 Enabled=false、无平台）时不得同步模型：
// 否则会清掉用户手写或 ccswitch 写入的 ANTHROPIC_*。登录标记仍应处理。
func TestSyncAgentModelsNoopWhenNeverConfigured(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"env": map[string]any{
		"ANTHROPIC_BASE_URL": "https://mine/",
		"ANTHROPIC_MODEL":    "mine",
	}})

	syncAgentModels(home, config.ModelConfig{}, nil)

	env := readSettingsEnv(t, home)
	if env["ANTHROPIC_BASE_URL"] != "https://mine/" || env["ANTHROPIC_MODEL"] != "mine" {
		t.Fatalf("未配置过平台模型时不得改动用户 env: %v", env)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("应仍处理登录标记: %v", err)
	}
	var onboarding map[string]any
	if err := json.Unmarshal(raw, &onboarding); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if onboarding["hasCompletedOnboarding"] != true {
		t.Fatalf("hasCompletedOnboarding = %v", onboarding["hasCompletedOnboarding"])
	}
}

// Home 为空时直接跳过，不 panic、不落盘。
func TestSyncAgentModelsNoHome(t *testing.T) {
	syncAgentModels("", config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", AnthropicBaseURL: "https://ant/"}},
	}, nil)
}

// 已安装但没有配置文件的工具也要同步（桌面端安装路径的等价行为）。
func TestSyncAgentModelsSyncsInstalledToolWithoutConfig(t *testing.T) {
	home := t.TempDir()
	mc := config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "OpenAI", OpenAIBaseURL: "https://oai/", APIKey: "k1",
			Models: []string{"gpt-x"},
		}},
	}

	syncAgentModels(home, mc, []discovery.Tool{{ID: "opencode", Installed: true}})

	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("已安装 OpenCode 应写入配置: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	prov, _ := doc["provider"].(map[string]any)
	if _, ok := prov["kshell-p1"]; !ok {
		t.Fatalf("provider 应含 kshell-p1: %v", prov)
	}
}

// 未安装且没有配置文件的工具不应被创建。
func TestSyncAgentModelsDoesNotCreateUninstalledTool(t *testing.T) {
	home := t.TempDir()
	mc := config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", OpenAIBaseURL: "https://oai/"}},
	}

	syncAgentModels(home, mc, nil)

	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("未安装 OpenCode 不应创建配置: err=%v", err)
	}
}

// 启动首轮扫描会同步平台模型，调用点被锁定；扫描本身不应报错。
func TestStartupScanCmdSyncsAgentModels(t *testing.T) {
	stubDetect(t, nil)
	home := t.TempDir()
	writeSettings(t, home, map[string]any{})
	o := claudeOptions(home, config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant/", APIKey: "k1", Models: []string{"m1"},
		}},
	})

	msg, ok := startupScanCmd(o)().(scanDoneMsg)
	if !ok {
		t.Fatal("startupScanCmd 应返回 scanDoneMsg")
	}
	if msg.err != nil {
		t.Fatalf("扫描不应报错: %v", msg.err)
	}
	if env := readSettingsEnv(t, home); env["ANTHROPIC_BASE_URL"] != "https://ant/" {
		t.Fatalf("启动扫描应写入平台端点: %v", env)
	}
}

// 同步写盘失败只影响同步本身，扫描照常返回。
func TestStartupScanCmdToleratesSyncFailure(t *testing.T) {
	stubDetect(t, nil)
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := claudeOptions(home, config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", AnthropicBaseURL: "https://ant/"}},
	})

	msg, ok := startupScanCmd(o)().(scanDoneMsg)
	if !ok || msg.err != nil {
		t.Fatalf("同步失败不应影响扫描: ok=%v err=%v", ok, msg.err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{broken" {
		t.Fatalf("同步失败不得改写坏文件: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("同步失败不得继续写 onboarding 标记: err=%v", err)
	}
}

// Init 必须走启动同步路径；改回 scanCmd 会让同步静默消失。
func TestInitSyncsAgentModels(t *testing.T) {
	stubDetect(t, nil)
	home := t.TempDir()
	writeSettings(t, home, map[string]any{})
	o := claudeOptions(home, config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", AnthropicBaseURL: "https://ant/"}},
	})

	m := NewModelWith(o)
	msg, ok := m.Init()().(scanDoneMsg)
	if !ok || msg.err != nil {
		t.Fatalf("Init 扫描应正常返回: ok=%v err=%v", ok, msg.err)
	}
	if env := readSettingsEnv(t, home); env["ANTHROPIC_BASE_URL"] != "https://ant/" {
		t.Fatalf("Init 应触发平台模型同步: %v", env)
	}
}

// 运行期重扫（刷新/会话结束）不再同步：避免用启动快照回滚桌面端刚写入的新配置。
func TestScanCmdDoesNotSyncAgentModels(t *testing.T) {
	stubDetect(t, nil)
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"env": map[string]any{"ANTHROPIC_BASE_URL": "https://mine/"}})
	o := claudeOptions(home, config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", AnthropicBaseURL: "https://ant/"}},
	})

	_ = scanCmd(o)()

	if env := readSettingsEnv(t, home); env["ANTHROPIC_BASE_URL"] != "https://mine/" {
		t.Fatalf("重扫不应写配置: %v", env)
	}
}
