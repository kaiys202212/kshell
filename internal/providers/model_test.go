package providers

import (
	"reflect"
	"testing"
)

func TestInjectModelMappings(t *testing.T) {
	cfg := ModelConfig{BaseURL: "https://h/", APIKey: "k", Model: "m"}

	args, env := Claude{}.InjectModel(cfg)
	if len(args) != 0 {
		t.Fatalf("claude args = %v", args)
	}
	wantClaude := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://h/",
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
	if env["OPENAI_BASE_URL"] != "https://h/" || env["OPENAI_API_KEY"] != "k" {
		t.Fatalf("codex env = %+v", env)
	}

	args, env = Gemini{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("gemini args = %v", args)
	}
	if env["GOOGLE_GEMINI_BASE_URL"] != "https://h/" || env["GEMINI_API_KEY"] != "k" {
		t.Fatalf("gemini env = %+v", env)
	}

	args, env = Opencode{}.InjectModel(cfg)
	if !reflect.DeepEqual(args, []string{"--model", "m"}) {
		t.Fatalf("opencode args = %v", args)
	}
	if len(env) != 0 {
		t.Fatalf("opencode 不应注入 env，got %+v", env)
	}
}

func TestInjectModelEmptyInjectsNothing(t *testing.T) {
	for _, p := range []interface {
		InjectModel(ModelConfig) ([]string, map[string]string)
	}{Claude{}, Codex{}, Gemini{}, Opencode{}} {
		args, env := p.InjectModel(ModelConfig{})
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("%T 空配置不应注入：args=%v env=%+v", p, args, env)
		}
	}
}
