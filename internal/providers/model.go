package providers

import "strings"

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
// 静态 settings.json 由 agentsync.Claude 写入 4 个槽位映射；ACP 会话级只
// 覆盖端点/密钥与「本次创建模型」，**不碰 DEFAULT_* 槽位**——原因见下方注释。
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
	// ANTHROPIC_MODEL 与 ANTHROPIC_DEFAULT_{SONNET,OPUS,HAIKU}_MODEL 的职责不同
	//（2026-11-22 对 claude-agent-acp 0.23.1 逐项实测）：
	//   - ANTHROPIC_MODEL 决定「新会话初始模型」；会话内 set_config_option
	//     之后由所选值接管，因此**可以**随创建模型下发（实测切换仍生效）。
	//   - DEFAULT_* 三个槽位是别名解析表（haiku → 平台真模型）。若把三个槽位
	//     钉成同一个具体模型，会话内切 sonnet/opus/haiku 拿到的都是那一个，
	//     表现为「切模型没反应」。槽位留给 agentsync 按平台写入静态
	//     settings.json，ACP 不覆盖。
	// 内置别名与裸名 "claude" 不能作为 ANTHROPIC_MODEL：该值不经别名解析，
	// 会被字面发往网关（400 "Model claude isn't included"）。
	if cfg.Model != "" && !IsClaudeUnpinnableModel(cfg.Model) {
		env["ANTHROPIC_MODEL"] = cfg.Model
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

// claudeBuiltinAliases 是 Claude CLI 能自行解析成真模型的内置别名。
// 依据：CLI 对 /model opus 回显 "Set model to opus (claude-opus-4-6)"（括号为
// 解析结果），对 /model claude 只回显 "Set model to claude"（无解析，字面透传）。
var claudeBuiltinAliases = []string{"default", "sonnet", "opus", "haiku"}

// IsClaudeBuiltinAlias 报告 s 是否为 Claude 内置模型别名（含 [1m] / -1m 变体）。
// 大小写不敏感、忽略首尾空白。与 agentsync 的槽位校正同口径，此处导出供
// desktop / modelresolve 复用，避免三处各写一份。
func IsClaudeBuiltinAlias(s string) bool {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return false
	}
	for _, base := range claudeBuiltinAliases {
		if v == base || v == base+"[1m]" || v == base+"-1m" {
			return true
		}
	}
	return false
}

// IsClaudeUnpinnableModel 报告 s 不可作为 ANTHROPIC_MODEL 下发。
// ANTHROPIC_MODEL 不经 CLI 的别名解析，写别名会被字面发往 API：内置别名
// （default/sonnet/opus/haiku 及变体）与裸名 "claude" 都属于此类，网关实测 400
//（"Model claude isn't included in ..."）。具体平台模型名不受限。
func IsClaudeUnpinnableModel(s string) bool {
	v := strings.TrimSpace(s)
	if v == "" {
		return false
	}
	return strings.EqualFold(v, "claude") || IsClaudeBuiltinAlias(v)
}
