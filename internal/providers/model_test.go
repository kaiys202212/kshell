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
		"ANTHROPIC_BASE_URL":             "https://ant.example/",
		"ANTHROPIC_AUTH_TOKEN":           "k",
		"ANTHROPIC_MODEL":                "m",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "m",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "m",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "m",
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
