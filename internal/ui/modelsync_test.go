package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/workbuddy"
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

// Anthropic 端点启用且 claude 定制同步开启时，按 4 槽位写入 settings.json env。
func TestSyncAgentModelsWritesClaudeSettings(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{})
	mc := config.ModelConfig{
		Enabled:         true,
		ActivePlatformID: "p1",
		ActiveModel:     "m-active",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "智谱",
			AnthropicBaseURL: "https://ant/", APIKey: "k1",
			Models: []string{"m1", "m2", "m3"},
		}},
		ToolSync: config.ToolSyncConfig{
			Enabled: map[string]bool{"claude": true},
			Claude: config.ClaudeSync{
				Model: "m-active", Sonnet: "m-active", Opus: "m1", Haiku: "m2",
			},
		},
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
		ActivePlatformID: "p1",
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
		ActivePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", Name: "OpenAI", OpenAIBaseURL: "https://oai/", APIKey: "k1",
			Models: []string{"gpt-x"},
		}},
		ToolSync: config.ToolSyncConfig{
			Enabled:  map[string]bool{"opencode": true},
			OpenCode: map[string][]string{"p1": {"gpt-x"}},
		},
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
		ActivePlatformID: "p1",
		Platforms: []config.ModelPlatform{{
			ID: "p1", AnthropicBaseURL: "https://ant/", APIKey: "k1", Models: []string{"m1"},
		}},
		ToolSync: config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
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
		ToolSync:  config.ToolSyncConfig{Enabled: map[string]bool{"claude": true}},
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

// tuiPerToolConfig 构造 TUI 每工具平台测试配置：全局激活 p2，codex 映射 p1。
// 端点形状与 modelresolve/desktop 测试保持一致，便于对照口径。
func tuiPerToolConfig() config.ModelConfig {
	return config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p2",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P1", OpenAIBaseURL: "https://o1/v1", AnthropicBaseURL: "https://a1/v1", APIKey: "k1"},
			{ID: "p2", Name: "P2", OpenAIBaseURL: "https://o2/v1", AnthropicBaseURL: "https://a2/v1", APIKey: "k2"},
		},
		ToolSync: config.ToolSyncConfig{ToolPlatforms: map[string]string{"codex": "p1"}},
	}
}

// tuiModelWithCodex 准备一个只有 codex 可用的 TUI 模型（含一个 codex 会话）。
func tuiModelWithCodex(cfg config.ModelConfig) Model {
	sessions := []providers.Session{{
		ID: "s-codex", ToolID: "codex", Workspace: `D:\ws-codex`,
		Title: "codex 会话", UpdatedAt: time.Now(),
	}}
	m := NewModelWith(Options{Config: config.Config{MaxDepth: 10, Model: cfg}})
	m.tools = []discovery.Tool{{ID: "codex", Installed: true, BinPath: "codex"}}
	m.workspaces = discovery.GroupSessions(sessions)
	m.sessions = sessions
	m.focus = focusWorkspaces
	return m
}

// fakeTUIProxier 是 modelresolve.Proxier 的测试替身，记录 Ensure 调用次数。
type fakeTUIProxier struct {
	base, token string
	calls       int
}

func (f *fakeTUIProxier) Ensure() (string, string, error) {
	f.calls++
	return f.base, f.token, nil
}

// TUI 模型注入必须按工具解析平台：codex 映射 p1 时，即便全局激活 p2，
// 新建会话也吃 p1 端点；恢复路径的来源同样以会话 ToolID 为准。
// 注意恢复路径本身维持 launch.ForSession「不注入模型」现状（见下方断言）。
func TestModelOptionsPerToolTUI(t *testing.T) {
	m := tuiModelWithCodex(tuiPerToolConfig())

	// 恢复路径的 modelOptions 来源必须是会话所属工具，而不是全局激活平台。
	s, ok := m.selectedSession()
	if !ok {
		t.Fatal("应选中 codex 会话")
	}
	mc, ok := m.modelOptions(s.ToolID).Resolver(s.ToolID)
	if !ok || mc.OpenAIBaseURL != "https://o1/v1" || mc.APIKey != "k1" {
		t.Fatalf("modelOptions(codex) = %+v ok=%v, want p1 端点", mc, ok)
	}

	// 新建会话经 PreferredTool 解析首选工具（codex），同样按 p1 注入环境变量。
	l, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if got := l.Env["OPENAI_BASE_URL"]; got != "https://o1/v1" {
		t.Fatalf("新建会话 OPENAI_BASE_URL = %q, want https://o1/v1（env=%v）", got, l.Env)
	}
	if l.Env["OPENAI_API_KEY"] != "k1" {
		t.Fatalf("新建会话 OPENAI_API_KEY = %q, want k1（env=%v）", l.Env["OPENAI_API_KEY"], l.Env)
	}

	// 边界：恢复路径沿用会话原模型，launch.ForSession 故意不注入模型端点。
	rl, err := m.resumeLaunch()
	if err != nil {
		t.Fatalf("resumeLaunch error: %v", err)
	}
	if v, ok := rl.Env["OPENAI_BASE_URL"]; ok {
		t.Fatalf("恢复会话不应注入模型端点，got %q（env=%v）", v, rl.Env)
	}
}

// 激活 workbuddy 平台时，TUI 的 Resolver 必须经注入的 Proxier 取端点与令牌。
func TestModelOptionsTUIUsesProxierForWorkbuddy(t *testing.T) {
	cfg := tuiPerToolConfig()
	cfg.ActivePlatformID = "wb"
	cfg.Platforms = append(cfg.Platforms, config.ModelPlatform{ID: "wb", Name: "WorkBuddy", Preset: workbuddy.PresetID})
	// 去掉 codex 映射，让解析回落到全局激活的 workbuddy 平台。
	delete(cfg.ToolSync.ToolPlatforms, "codex")
	m := tuiModelWithCodex(cfg)
	px := &fakeTUIProxier{base: "http://127.0.0.1:9999", token: "tk"}
	m.modelProxy = px

	l, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if px.calls == 0 {
		t.Fatal("workbuddy 平台应经 Proxier.Ensure 取端点")
	}
	if l.Env["OPENAI_BASE_URL"] != px.base || l.Env["OPENAI_API_KEY"] != px.token {
		t.Fatalf("env = %v, want 代理端点 %q / 令牌 %q", l.Env, px.base, px.token)
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
