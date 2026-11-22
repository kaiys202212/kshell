package agentsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
)

// Claude 把默认平台的 Anthropic 端点/密钥/模型写进 ~/.claude/settings.json 的 env 块。
// settings.json 只有一份 env，无法承载多平台——静态注入只覆盖默认平台
// （即 ccswitch 的「切平台」语义）；kshell 关闭或无 Anthropic 端点时删除
// 管理键恢复无注入状态。
//
// 模型：由 ToolSync.Claude 的 4 个槽位（默认/Sonnet/Opus/Haiku）显式映射，
// 槽位非空才写对应键，全空 = 只清不写；不再从 ActiveModel 或平台列表隐式推导。
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
// （与 ccswitch 行为一致）。含模型键，避免 settings.json 残留 mimo 等旧值。
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
	// 从未配置过模型：settings.json 整份不碰（不读、不建、不改写），只补写
	// 登录标记。否则「先删后建」会清掉用户或 ccswitch 写入的 ANTHROPIC_*，
	// 把本来可用的 Claude Code 弄坏。
	if !in.Managed() {
		skipOnboarding(home)
		return nil
	}
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
		if p := pickClaudePlatform(in); p != nil && strings.TrimSpace(p.AnthropicBaseURL) != "" {
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
// 会写入）：配置了槽位模型时，若该值既非内置别名、也不在任何非空槽位中，
// 视为旧平台残留并删除，避免 Custom 悬空。无任何槽位时不校验（自由输入）。
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
	slots := claudeSlotModels(in)
	if len(slots) == 0 {
		return
	}
	bare := bareModelID(cur)
	for _, m := range slots {
		if cur == m || bare == m {
			return
		}
	}
	delete(doc, "model")
}

func isClaudeBuiltinModelAlias(s string) bool {
	// 与 providers.IsClaudeBuiltinAlias 同口径；此处保留薄封装以免
	// agentsync 的所有调用点都改 import 别名，实际判定逻辑单一来源。
	return providers.IsClaudeBuiltinAlias(s)
}

// claudeModelManaged 报告默认平台是否配置了 Anthropic 端点——只有此时 kshell
// 才接管 Claude 的模型键与顶层 model。仅配 OpenAI 协议的平台不能把它的模型名
// 塞给 Claude（官方端点会报模型不存在），也不能按它的列表校验用户自己选定的
// 顶层 model。
func claudeModelManaged(in SyncInput) bool {
	p := pickClaudePlatform(in)
	return p != nil && strings.TrimSpace(p.AnthropicBaseURL) != ""
}

// claudeSlotModels 返回 ToolSync.Claude 非空槽位值（TrimSpace 后）。
func claudeSlotModels(in SyncInput) []string {
	c := in.ToolSync.Claude
	var out []string
	for _, v := range []string{c.Model, c.Sonnet, c.Opus, c.Haiku} {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// writeModelEnv 按 ToolSync.Claude 的 4 个槽位写入模型 env 键。
//
// 槽位语义（2026-11-22 实测定档，见 docs/plans/2026-11-22-acp-model-fix-design.md）：
//   - ANTHROPIC_MODEL 是「新会话初始模型」；
//   - ANTHROPIC_DEFAULT_{SONNET,OPUS,HAIKU}_MODEL 是别名解析表，会话内切
//     sonnet/opus/haiku 时按槽位取具体模型。
//
// 由此得出两条硬约束：
//  1. **三个槽位必须都有值**。空槽位会让 Claude 回落到内置真模型名
//     （claude-sonnet-4-6），发往第三方网关必然 400（实测）。
//  2. 槽位值全相同会让三个别名指向同一模型，用户切哪个都没变化（实测）。
//     这取决于用户的分档映射，kshell 不替用户编造差异，只如实写入。
//
// 因此这里用「首个非空槽位」补齐空缺槽位，保证别名表可解析、不 400；
// 想要真正的分档切换需在设置页把三个槽位映射到不同模型。
// "default" 是 ACP 的「让 agent 自选」占位值，经环境变量进入 CLI 不被解析，
// 会字面发往 API（网关 400），故按未填处理。
func writeModelEnv(env map[string]any, in SyncInput) {
	c := in.ToolSync.Claude
	model := cleanClaudeSlotValue(c.Model)
	sonnet := cleanClaudeSlotValue(c.Sonnet)
	opus := cleanClaudeSlotValue(c.Opus)
	haiku := cleanClaudeSlotValue(c.Haiku)

	if model != "" {
		env["ANTHROPIC_MODEL"] = model
	}
	fallback := firstNonEmpty(model, sonnet, opus, haiku)
	if fallback == "" {
		return // 四个槽位全空：只清不写（模型键已在 Sync 开头统一删除）
	}
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = firstNonEmpty(sonnet, fallback)
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = firstNonEmpty(opus, fallback)
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = firstNonEmpty(haiku, fallback)
}

// cleanClaudeSlotValue 归一槽位值：trim 后为空或 "default"（含大小写）视为未填。
func cleanClaudeSlotValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "default") {
		return ""
	}
	return v
}

// firstNonEmpty 返回首个非空参数；全空返回空串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// pickPlatform 按显式平台 ID 查找，未命中或空取第一个；无平台返回 nil。
func pickPlatform(in SyncInput, id string) *config.ModelPlatform {
	if len(in.Platforms) == 0 {
		return nil
	}
	id = strings.TrimSpace(id)
	if id != "" {
		for i := range in.Platforms {
			if in.Platforms[i].ID == id {
				return &in.Platforms[i]
			}
		}
	}
	return &in.Platforms[0]
}

// pickClaudePlatform 按 ClaudePlatformID 查找，空/悬空回落第一个。
func pickClaudePlatform(in SyncInput) *config.ModelPlatform {
	return pickPlatform(in, in.ClaudePlatformID)
}
