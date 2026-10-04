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

// Opencode：仅 --model（期望 provider/model）；端点/密钥无稳定 env 注入方式，v1 不支持。
func (Opencode) InjectModel(cfg ModelConfig) ([]string, map[string]string) {
	var args []string
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	return args, nil
}
