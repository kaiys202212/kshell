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

// SettingsEnvInjector 可选接口：把模型配置经 `--settings` 的 env 块注入。
// 背景：claude CLI 会用用户级 settings.json 的 env 块**替换**进程继承的
// 同名环境变量（官方 env-vars 文档 precedence 一节），走进程环境注入模型
// 会被用户自己的 ANTHROPIC_* 配置覆盖；`--settings`（命令行层）优先级高于
// 用户级 settings.json，可稳定压过。
type SettingsEnvInjector interface {
	SettingsEnv(cfg ModelConfig) map[string]string
}

// Claude 模型注入改走 settings env 块：返回需注入的环境变量（只含非空字段）。
// 模型同步三个 DEFAULT 别名，兼容只认单一模型的代理端点。
func (Claude) SettingsEnv(cfg ModelConfig) map[string]string {
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
	return env
}

// Claude.InjectModel 保留接口实现但不再产出 env：进程环境注入对 claude 已被
// 证实会被用户 settings.json 覆盖，保留只会造成"看似注入了"的假象。
func (Claude) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	return nil, nil
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

// Cursor：仅 `--model`。不注入 CURSOR_API_KEY——设置页共享密钥是代理端点用的，
// 不是 Cursor 账号 key；注入后 CLI 会报 invalid API key 并立刻退出。
func (Cursor) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	if cfg.Model == "" {
		return nil, nil
	}
	return []string{"--model", cfg.Model}, nil
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
