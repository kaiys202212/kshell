package providers

import (
	"encoding/json"
	"strings"
)

// ModelConfig 是按工具适配后的模型注入值（双协议端点/密钥 + 该工具的模型名）。
type ModelConfig struct {
	OpenAIBaseURL    string
	AnthropicBaseURL string
	APIKey           string
	Model            string
}

// ModelInjector 可选接口：把模型配置翻译成该工具认的启动参数与环境变量。
// 只注入非空字段；工具不支持的字段忽略（见 docs/plans/2026-10-03-model-config-design.md §4）。
type ModelInjector interface {
	InjectModel(cfg ModelConfig) (args []string, env map[string]string)
}

// Claude：ANTHROPIC_* 环境变量；模型同步三个 DEFAULT 别名，兼容只认单一模型的代理端点。
func (Claude) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	env := map[string]string{}
	if cfg.AnthropicBaseURL != "" {
		env["ANTHROPIC_BASE_URL"] = cfg.AnthropicBaseURL
	}
	if cfg.APIKey != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = cfg.APIKey
	}
	if cfg.Model != "" {
		env["ANTHROPIC_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = cfg.Model
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = cfg.Model
	}
	return nil, env
}

// Codex：-m <model> + OPENAI_* 环境变量。
func (Codex) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	if cfg.OpenAIBaseURL != "" {
		env["OPENAI_BASE_URL"] = cfg.OpenAIBaseURL
	}
	if cfg.APIKey != "" {
		env["OPENAI_API_KEY"] = cfg.APIKey
	}
	return args, env
}

// Gemini：--model <model> + GEMINI_API_KEY / GOOGLE_GEMINI_BASE_URL；端点优先 OpenAI URL。
func (Gemini) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	base := cfg.OpenAIBaseURL
	if base == "" {
		base = cfg.AnthropicBaseURL
	}
	if base != "" {
		env["GOOGLE_GEMINI_BASE_URL"] = base
	}
	if cfg.APIKey != "" {
		env["GEMINI_API_KEY"] = cfg.APIKey
	}
	return args, env
}

// Cursor：官方 CLI `--model`；可选 CURSOR_API_KEY（无独立端点映射）。
func (Cursor) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.APIKey != "" {
		env["CURSOR_API_KEY"] = cfg.APIKey
	}
	return args, env
}

// CodeBuddy：`--model` + CODEBUDDY_MODEL（文档约定 env 覆盖默认模型）。
func (CodeBuddy) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	env := map[string]string{}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
		env["CODEBUDDY_MODEL"] = cfg.Model
	}
	return args, env
}

// Opencode：需要 provider/model。裸模型名且有 BaseURL 时经 OPENCODE_CONFIG_CONTENT
// 注册临时 openai-compatible provider `kshell`，避免非法 --model 破坏启动/续聊。
func (Opencode) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, nil
	}

	// 已是 provider/model：只传 CLI flag。
	if strings.Contains(model, "/") {
		return []string{"--model", model}, nil
	}

	base := strings.TrimSpace(cfg.OpenAIBaseURL)
	if base == "" {
		base = strings.TrimSpace(cfg.AnthropicBaseURL)
	}
	// 裸名且无端点：不注入 --model（OpenCode 会拒识或开出空会话）。
	if base == "" {
		return nil, nil
	}

	full := "kshell/" + model
	content, err := json.Marshal(map[string]any{
		"provider": map[string]any{
			"kshell": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "kshell",
				"options": map[string]any{
					"baseURL": base,
					"apiKey":  cfg.APIKey,
				},
				"models": map[string]any{
					model: map[string]any{"name": model},
				},
			},
		},
		"model": full,
	})
	if err != nil {
		return nil, nil
	}
	return []string{"--model", full}, map[string]string{
		"OPENCODE_CONFIG_CONTENT": string(content),
	}
}
