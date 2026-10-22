package providers

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

// CodeBuddy：平台模型改由 agentsync 写入 ~/.codebuddy/models.json，
// 启动时不再注入 --model / CODEBUDDY_MODEL，避免与自有配置双通道打架。
func (CodeBuddy) InjectModel(ModelConfig) ([]string, map[string]string) {
	return nil, nil
}

// Opencode：平台模型改由 agentsync 写入 opencode.json 的 provider.kshell-*，
// 启动时不再注入 OPENCODE_CONFIG_CONTENT / --model。
func (Opencode) InjectModel(ModelConfig) ([]string, map[string]string) {
	return nil, nil
}
