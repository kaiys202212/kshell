package providers

import (
	"encoding/json"
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
	if len(args) != 0 {
		t.Fatalf("claude args = %v", args)
	}
	wantClaude := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://ant.example/",
		"ANTHROPIC_AUTH_TOKEN":           "k",
		"ANTHROPIC_MODEL":                "m",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "m",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "m",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "m",
	}
	if !reflect.DeepEqual(env, wantClaude) {
		t.Fatalf("claude env = %+v", env)
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
	if env["CURSOR_API_KEY"] != "k" {
		t.Fatalf("cursor env = %+v", env)
	}

	args, env = CodeBuddy{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("codebuddy args = %v", args)
	}
	if env["CODEBUDDY_MODEL"] != "m" {
		t.Fatalf("codebuddy env = %+v", env)
	}

	args, env = Opencode{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "kshell/m"}) {
		t.Fatalf("opencode args = %v", args)
	}
	raw := env["OPENCODE_CONFIG_CONTENT"]
	if raw == "" {
		t.Fatal("opencode 应注入 OPENCODE_CONFIG_CONTENT")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("CONFIG_CONTENT JSON: %v", err)
	}
	if doc["model"] != "kshell/m" {
		t.Fatalf("config model = %v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	kshell, _ := prov["kshell"].(map[string]any)
	opts, _ := kshell["options"].(map[string]any)
	if opts["baseURL"] != "https://oai.example/" || opts["apiKey"] != "k" {
		t.Fatalf("kshell options = %+v", opts)
	}
}

func TestInjectModelOpencodeProviderSlash(t *testing.T) {
	args, env := Opencode{}.InjectModel(ModelConfig{Model: "openai/gpt-4o"})
	if !reflect.DeepEqual(args, []string{"--model", "openai/gpt-4o"}) {
		t.Fatalf("args = %v", args)
	}
	if len(env) != 0 {
		t.Fatalf("已带 provider/ 时不应写 CONFIG_CONTENT，got %+v", env)
	}
}

func TestInjectModelOpencodeBareWithoutBaseURL(t *testing.T) {
	args, env := Opencode{}.InjectModel(ModelConfig{Model: "glm-5.3-flash"})
	if len(args) != 0 || len(env) != 0 {
		t.Fatalf("裸名无端点不应注入：args=%v env=%+v", args, env)
	}
}

func TestInjectModelClaudeIgnoresOpenAIURL(t *testing.T) {
	_, env := Claude{}.InjectModel(ModelConfig{OpenAIBaseURL: "https://oai/", AnthropicBaseURL: ""})
	if env["ANTHROPIC_BASE_URL"] != "" {
		t.Fatalf("无 anthropic URL 时不应注入，got %+v", env)
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
