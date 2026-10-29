package agentsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
)

// Claude 把默认平台的 Anthropic 端点/密钥/模型写进 ~/.claude/settings.json 的 env 块。
// settings.json 只有一份 env，无法承载多平台——静态注入只覆盖默认平台
//（即 ccswitch 的「切平台」语义）；kshell 关闭或无 Anthropic 端点时删除
// 管理键恢复无注入状态。
//
// 模型：ANTHROPIC_MODEL = ActiveModel（空则平台列表首项）；Sonnet/Opus/Haiku
// 三个 DEFAULT 别名依次映射平台模型列表前三项（不足则回落 ActiveModel），
// 让 Claude TUI /model 能切到 kshell 配置的模型，而不是 settings 里陈旧值。
// 模型键与顶层 model 校正仅当默认平台配置了 Anthropic 端点时才启用——只有
// OpenAI 协议的平台不接管 Claude（否则 OpenAI 模型名会被发给官方端点）。
type Claude struct{}

func (Claude) ToolID() string { return "claude" }

func (Claude) configPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

func (c Claude) ConfigExists(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil
}

// kshellEnvKeys 是 kshell 静态注入接管的固定键；先删后建，用户同名键会被覆盖
//（与 ccswitch 行为一致）。含模型键，避免 settings.json 残留 mimo 等旧值。
//
// 新版 Claude Code 为 sonnet/opus/haiku/fable 四别名引入 _MODEL_NAME/
// _MODEL_DESCRIPTION/_MODEL_SUPPORTED_CAPABILITIES 显示名键：实际请求走
// _MODEL(ID) 键，但 /model 列表 label 优先取 _NAME，旧平台残留会让用户误以为
// 模型没切换。这组键 kshell 只清理不代写——不设时 Claude 会从模型 ID 自动
// 推导显示名。ANTHROPIC_DEFAULT_FABLE_MODEL 同理只清理：kshell 平台列表尚未
// 映射 Fable 别名，残留会把 /model 的 Fable 条目指向旧平台模型。
var kshellEnvKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_NAME",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_NAME",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_DESCRIPTION",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES",
}

func (c Claude) Sync(home string, in SyncInput) error {
	path := c.configPath(home)
	doc, err := readJSONObject(path)
	if err != nil {
		return err
	}
	env, _ := doc["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for _, k := range kshellEnvKeys {
		delete(env, k)
	}
	if in.Enabled {
		if p := pickPlatform(in); p != nil && strings.TrimSpace(p.AnthropicBaseURL) != "" {
			env["ANTHROPIC_BASE_URL"] = p.AnthropicBaseURL
			if p.APIKey != "" {
				env["ANTHROPIC_AUTH_TOKEN"] = p.APIKey
			}
			writeModelEnv(env, in)
		}
	}
	if len(env) > 0 {
		doc["env"] = env
	} else {
		delete(doc, "env")
	}
	if claudeModelManaged(in) {
		reconcileClaudeTopLevelModel(doc, in)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := writeAtomic(path, raw); err != nil {
		return err
	}
	skipOnboarding(home)
	return nil
}

// skipOnboarding 把 ~/.claude.json 的 hasCompletedOnboarding 置 true，跳过
// 新版 Claude Code 首启的登录/引导页。与注入 Enabled 无关：新装未配平台时
// 同样不该被引导页卡住。已是 true 不重写（文件可能很大）；损坏或不可读时
// 静默跳过，不碰文件。错误不外抛——引导标记失败不应阻断 settings 同步。
func skipOnboarding(home string) {
	path := filepath.Join(home, ".claude.json")
	doc := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(b))) > 0 {
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.UseNumber() // 大整型时间戳经 float64 会丢精度
			if err := dec.Decode(&doc); err != nil || doc == nil {
				return
			}
		}
	} else if !os.IsNotExist(err) {
		return
	}
	if v, ok := doc["hasCompletedOnboarding"].(bool); ok && v {
		return
	}
	doc["hasCompletedOnboarding"] = true
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	_ = writeAtomic(path, raw)
}

// reconcileClaudeTopLevelModel 校正 settings.json 顶层 "model"（Claude /model
// 会写入）：默认平台有模型列表时，若该值既非内置别名、也不在列表/ActiveModel
// 中，视为旧平台残留并删除，避免 Custom 悬空。无列表时不校验（自由输入）。
func reconcileClaudeTopLevelModel(doc map[string]any, in SyncInput) {
	cur, ok := doc["model"].(string)
	if !ok {
		return
	}
	cur = strings.TrimSpace(cur)
	if cur == "" {
		delete(doc, "model")
		return
	}
	if isClaudeBuiltinModelAlias(cur) {
		return
	}
	models := platformModelNames(in)
	if len(models) == 0 {
		return
	}
	active := strings.TrimSpace(in.ActiveModel)
	bare := bareModelID(cur)
	for _, m := range models {
		if cur == m || bare == m {
			return
		}
	}
	if active != "" && (cur == active || bare == active) {
		return
	}
	delete(doc, "model")
}

func isClaudeBuiltinModelAlias(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "default", "sonnet", "opus", "haiku",
		"sonnet[1m]", "opus[1m]", "haiku[1m]",
		"sonnet-1m", "opus-1m", "haiku-1m":
		return true
	default:
		return false
	}
}

// claudeModelManaged 报告默认平台是否配置了 Anthropic 端点——只有此时 kshell
// 才接管 Claude 的模型键与顶层 model。仅配 OpenAI 协议的平台不能把它的模型名
// 塞给 Claude（官方端点会报模型不存在），也不能按它的列表校验用户自己选定的
// 顶层 model。
func claudeModelManaged(in SyncInput) bool {
	p := pickPlatform(in)
	return p != nil && strings.TrimSpace(p.AnthropicBaseURL) != ""
}

// writeModelEnv 写入 ANTHROPIC_MODEL 与三个 DEFAULT 别名；无可用模型名则不写。
//
// Sonnet 取 ActiveModel（空则列表首项），Opus/Haiku 顺延到排除 ActiveModel 后的
// 列表项。Claude 顶层 "model":"sonnet" 会解析到 SONNET 别名；若这里按固定下标
// 取列表首项，用户在 kshell 选的 active_model 就会被无视，可能请求到未选模型。
func writeModelEnv(env map[string]any, in SyncInput) {
	ordered := modelAliasOrder(platformModelNames(in), strings.TrimSpace(in.ActiveModel))
	if len(ordered) == 0 {
		return
	}
	env["ANTHROPIC_MODEL"] = ordered[0]
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = ordered[0]
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = aliasAt(ordered, 1)
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = aliasAt(ordered, 2)
}

// modelAliasOrder 返回 [active] + 排除 active 的模型列表（保持原顺序）。
// active 为空时直接返回列表，保持原有回落语义。
func modelAliasOrder(models []string, active string) []string {
	ordered := make([]string, 0, len(models)+1)
	if active != "" {
		ordered = append(ordered, active)
	}
	for _, m := range models {
		if m == active {
			continue
		}
		ordered = append(ordered, m)
	}
	return ordered
}

// aliasAt 取下标项，不足时回落首项（Opus/Haiku 没有更多模型可用）。
func aliasAt(ordered []string, i int) string {
	if i < len(ordered) {
		return ordered[i]
	}
	return ordered[0]
}

// platformModelNames 取默认平台的非空模型名列表（顺序保留）。
func platformModelNames(in SyncInput) []string {
	p := pickPlatform(in)
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Models))
	for _, m := range p.Models {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

// pickPlatform 按 DefaultPlatformID 线性查找，未命中或空取第一个；无平台返回 nil。
func pickPlatform(in SyncInput) *config.ModelPlatform {
	if len(in.Platforms) == 0 {
		return nil
	}
	id := strings.TrimSpace(in.DefaultPlatformID)
	if id != "" {
		for i := range in.Platforms {
			if in.Platforms[i].ID == id {
				return &in.Platforms[i]
			}
		}
	}
	return &in.Platforms[0]
}
