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

// SettingsEnvInjector 可选接口：产出模型相关 env 映射（只含非空字段）。
// 终端路径改由 agentsync 静态写入 ~/.claude/settings.json，不再经 argv
// `--settings` 临时注入；本接口仍供 ACP 路径（acpSettingsPayload → _meta）
// 直接调用 Claude.SettingsEnv，以支持会话内跨平台切端点。
type SettingsEnvInjector interface {
	SettingsEnv(cfg ModelConfig) map[string]string
}

// Claude.SettingsEnv 返回 ACP _meta 载荷用的 env（只含非空字段）。
// 模型同步三个 DEFAULT 别名，兼容只认单一模型的代理端点。
// 静态 settings.json 由 agentsync.Claude 写入同名键（含 ActiveModel 与
// 平台列表映射的 DEFAULT 别名）；ACP 会话级仍可经本方法覆盖。
func (Claude) SettingsEnv(cfg ModelConfig) map[string]string {
	env := map[string]string{}
	// 无 Anthropic 端点时整组不注入：只配 OpenAI 协议的平台，其 key/模型名
	// 不能作为 ANTHROPIC_* 发往官方端点（401/模型不存在）。
	if cfg.AnthropicBaseURL == "" {
		return env
	}
	env["ANTHROPIC_BASE_URL"] = cfg.AnthropicBaseURL
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

// Claude.InjectModel 保留接口实现但不再产出：端点/密钥走静态 settings.json，
// 会话模型走 ACP SettingsEnv；进程 env 注入会被用户 settings.json 覆盖。
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
