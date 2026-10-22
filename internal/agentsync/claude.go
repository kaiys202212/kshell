package agentsync

import (
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
var kshellEnvKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
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
		}
		writeModelEnv(env, in)
	}
	if len(env) > 0 {
		doc["env"] = env
	} else {
		delete(doc, "env")
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(path, raw)
}

// writeModelEnv 写入 ANTHROPIC_MODEL 与三个 DEFAULT 别名；无可用模型名则不写。
func writeModelEnv(env map[string]any, in SyncInput) {
	models := platformModelNames(in)
	active := strings.TrimSpace(in.ActiveModel)
	if active == "" && len(models) > 0 {
		active = models[0]
	}
	if active == "" {
		return
	}
	env["ANTHROPIC_MODEL"] = active
	sonnet, opus, haiku := active, active, active
	if len(models) > 0 {
		sonnet = models[0]
	}
	if len(models) > 1 {
		opus = models[1]
	}
	if len(models) > 2 {
		haiku = models[2]
	}
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = sonnet
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = opus
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = haiku
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
