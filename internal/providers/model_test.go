package providers

import (
	"reflect"
	"testing"
)

func TestInjectModelMappings(t *testing.T) {
	cfg := ModelConfig{
		OpenAIBaseURL:    "https://oai.example/",
		AnthropicBaseURL: "https://ant.example/",
		APIKey:           "k",
		Model:            "m",
	}

	args, env := Claude{}.InjectModel(cfg)
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("claude InjectModel 应不再产出 env（改走 SettingsEnv）: args=%v env=%+v", args, env)
	}
	wantClaude := map[string]string{
		"ANTHROPIC_BASE_URL":   "https://ant.example/",
		"ANTHROPIC_AUTH_TOKEN": "k",
		"ANTHROPIC_MODEL":      "m",
		// 刻意不含 ANTHROPIC_DEFAULT_{SONNET,OPUS,HAIKU}_MODEL：槽位是别名解析表，
		// 由 agentsync 按平台写进静态 settings.json。ACP 若把三个槽位钉成同一个
		// 具体模型，会话内切 sonnet/opus/haiku 会全部落回该模型（切模型没反应）。
	}
	env = Claude{}.SettingsEnv(cfg)
	if !reflect.DeepEqual(env, wantClaude) {
		t.Fatalf("claude SettingsEnv = %+v", env)
	}

	args, env = Codex{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"-m", "m"}) {
		t.Fatalf("codex args = %v", args)
	}
	if env["OPENAI_BASE_URL"] != "https://oai.example/" || env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("codex env = %+v", env)
	}

	args, env = Gemini{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("gemini args = %v", args)
	}
	if env["GOOGLE_GEMINI_BASE_URL"] != "https://oai.example/" || env["GEMINI_API_KEY"] != "k" {
		t.Fatalf("gemini env = %+v", env)
	}

	args, env = Cursor{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("cursor args = %v", args)
	}
	if len(env) != 0 {
		t.Fatalf("cursor 不应注入共享 API Key，got %+v", env)
	}

	// OpenCode / CodeBuddy：改走 agentsync 持久配置，启动注入为空。
	args, env = CodeBuddy{}.InjectModel(cfg)
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("codebuddy 不应再启动注入: args=%v env=%+v", args, env)
	}
	args, env = Opencode{}.InjectModel(cfg)
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("opencode 不应再启动注入: args=%v env=%+v", args, env)
	}
}

func TestInjectModelOpencodeNoLongerInjects(t *testing.T) {
	args, env := Opencode{}.InjectModel(ModelConfig{Model: "openai/gpt-4o", OpenAIBaseURL: "https://h/"})
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("opencode 应空注入: args=%v env=%+v", args, env)
	}
	args, env = Opencode{}.InjectModel(ModelConfig{Model: "glm-5.3-flash"})
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("opencode 应空注入: args=%v env=%+v", args, env)
	}
}

func TestInjectModelClaudeIgnoresOpenAIURL(t *testing.T) {
	env := Claude{}.SettingsEnv(ModelConfig{OpenAIBaseURL: "https://oai/", AnthropicBaseURL: ""})
	if env["ANTHROPIC_BASE_URL"] != "" {
		t.Fatalf("无 anthropic URL 时不应注入，got %+v", env)
	}
	// 无 Anthropic 端点时整组不注入：OpenAI 的 key/模型名不得被当作
	// ANTHROPIC_AUTH_TOKEN/ANTHROPIC_MODEL 发往官方端点。
	env = Claude{}.SettingsEnv(ModelConfig{
		OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "",
		APIKey: "sk-oai", Model: "gpt-5",
	})
	if len(env) != 0 {
		t.Fatalf("无 anthropic URL 时应返回空 env，got %+v", env)
	}
}

// TestClaudeSettingsEnvSkipsDefaultModel 锁定伪模型防线：default 是 Claude ACP
// 的「让 agent 自选」占位值，写进 ANTHROPIC_MODEL 会被 CLI 字面发给 API
// （网关 400）。此时只注入端点/密钥，模型键整组不写。
func TestClaudeSettingsEnvSkipsDefaultModel(t *testing.T) {
	for _, model := range []string{"default", " Default "} {
		env := Claude{}.SettingsEnv(ModelConfig{
			AnthropicBaseURL: "https://ant/", APIKey: "k", Model: model,
		})
		want := map[string]string{
			"ANTHROPIC_BASE_URL":   "https://ant/",
			"ANTHROPIC_AUTH_TOKEN": "k",
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("SettingsEnv(Model=%q) = %+v, want %+v", model, env, want)
		}
	}
}

// TestClaudeSettingsEnvSkipsClaudeLiteral 锁定 "claude" 这个裸名同样不可钉死。
// 现场事故：会话内 `/model claude` 把字面值写进配置后，ANTHROPIC_MODEL=claude
// 被原样发给 OpenCode Go 网关，返回 400 "Model claude isn't included"。
func TestClaudeSettingsEnvSkipsClaudeLiteral(t *testing.T) {
	for _, model := range []string{"claude", "Claude", " claude "} {
		env := Claude{}.SettingsEnv(ModelConfig{
			AnthropicBaseURL: "https://ant/", APIKey: "k", Model: model,
		})
		want := map[string]string{
			"ANTHROPIC_BASE_URL":   "https://ant/",
			"ANTHROPIC_AUTH_TOKEN": "k",
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("SettingsEnv(Model=%q) = %+v, want %+v", model, env, want)
		}
	}
}

// TestClaudeSettingsEnvSkipsBuiltinAliases 别名同样不可钉死：ANTHROPIC_MODEL
// 不经 CLI 别名解析，写 sonnet 会被字面发往 API。具体平台模型名才写模型键。
func TestClaudeSettingsEnvSkipsBuiltinAliases(t *testing.T) {
	for _, model := range []string{"sonnet", "opus", "haiku", "sonnet[1m]", "opus-1m"} {
		env := Claude{}.SettingsEnv(ModelConfig{
			AnthropicBaseURL: "https://ant/", APIKey: "k", Model: model,
		})
		if _, ok := env["ANTHROPIC_MODEL"]; ok {
			t.Fatalf("SettingsEnv(Model=%q) 不应注入 ANTHROPIC_MODEL: %+v", model, env)
		}
		if _, ok := env["ANTHROPIC_DEFAULT_SONNET_MODEL"]; ok {
			t.Fatalf("SettingsEnv(Model=%q) 不应注入 DEFAULT_* 模型键: %+v", model, env)
		}
	}
}

// TestClaudeSettingsEnvInjectsConcreteModel 具体模型名写入 ANTHROPIC_MODEL，
// 但**不覆盖** DEFAULT_* 槽位：槽位是别名解析表（由 agentsync 按平台同步到
// 静态 settings.json），ACP 覆盖会让会话内切 sonnet/opus/haiku 全部失效。
func TestClaudeSettingsEnvInjectsConcreteModel(t *testing.T) {
	env := Claude{}.SettingsEnv(ModelConfig{
		AnthropicBaseURL: "https://ant/", APIKey: "k", Model: "glm-5.3-flash",
	})
	if env["ANTHROPIC_MODEL"] != "glm-5.3-flash" {
		t.Fatalf("ANTHROPIC_MODEL = %q, want glm-5.3-flash（env=%+v）", env["ANTHROPIC_MODEL"], env)
	}
	for _, key := range []string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	} {
		if _, ok := env[key]; ok {
			t.Fatalf("不应覆盖槽位 %s（会话内切别名会失效）: %+v", key, env)
		}
	}
}

func TestInjectModelEmptyInjectsNothing(t *testing.T) {
	for _, p := range []interface {
		InjectModel(ModelConfig) ([]string, map[string]string)
	}{Claude{}, Codex{}, Gemini{}, Opencode{}, Cursor{}, CodeBuddy{}} {
		args, env := p.InjectModel(ModelConfig{})
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("%T 空配置不应注入：args=%v env=%+v", p, args, env)
		}
	}
}

func TestModelPresetsIncludeCustomAndDualProtocol(t *testing.T) {
	list := ModelPresets()
	if len(list) < 5 {
		t.Fatalf("预设过少: %d", len(list))
	}
	p, ok := FindModelPreset("minimax-token-plan")
	if !ok || p.OpenAIBaseURL == "" || p.AnthropicBaseURL == "" {
		t.Fatalf("minimax-token-plan 应含双协议 URL: %+v", p)
	}
	if _, ok := FindModelPreset("custom"); !ok {
		t.Fatal("缺 custom 预设")
	}
}
