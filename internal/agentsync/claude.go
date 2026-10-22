package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
)

// Claude 把默认平台的 Anthropic 端点/密钥写进 ~/.claude/settings.json 的 env 块。
// settings.json 只有一份 env，无法承载多平台——静态注入只覆盖默认平台
//（即 ccswitch 的「切平台」语义）；kshell 关闭或无 Anthropic 端点时删除
// 管理键恢复无注入状态。不写 ANTHROPIC_MODEL：模型由 agent 自选。
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
//（与 ccswitch 行为一致）。
var kshellEnvKeys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN"}

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
