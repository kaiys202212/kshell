package agentsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// OpenCode 把平台模型合并进 ~/.config/opencode/opencode.json 的 provider.kshell-*。
type OpenCode struct{}

func (OpenCode) ToolID() string { return "opencode" }

func (OpenCode) configPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

func (o OpenCode) ConfigExists(home string) bool {
	_, err := os.Stat(o.configPath(home))
	return err == nil
}

func (o OpenCode) Sync(home string, in SyncInput) error {
	// 从未配置过模型：opencode.json 原样保留，不删 kshell-* 残留也不补默认键。
	if !in.Managed() {
		return nil
	}
	path := o.configPath(home)
	doc, err := readJSONObject(path)
	if err != nil {
		return err
	}

	prov, _ := doc["provider"].(map[string]any)
	if prov == nil {
		prov = map[string]any{}
	}

	// 先清全部 kshell-*，再按当前平台重建（Enabled=false 则只清不建）。
	for k := range prov {
		if isKshellProviderKey(k) {
			delete(prov, k)
		}
	}

	if in.Enabled {
		for _, p := range in.Platforms {
			base := strings.TrimSpace(p.OpenAIBaseURL)
			if base == "" {
				continue
			}
			models := map[string]any{}
			for _, m := range p.Models {
				m = strings.TrimSpace(m)
				if m == "" {
					continue
				}
				// OpenCode 对目录外自定义模型的能力位默认保守（attachment=false），
				// 平台模型实际都具备这些能力，这里显式全开，放开图片/推理/工具调用。
				models[m] = map[string]any{
					"name":       m,
					"attachment": true,
					"reasoning":  true,
					"tool_call":  true,
				}
			}
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = p.ID
			}
			prov[providerKey(p.ID)] = map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": name,
				"options": map[string]any{
					"baseURL": base,
					"apiKey":  p.APIKey,
				},
				"models": models,
			}
		}
	}

	doc["provider"] = prov
	if _, ok := doc["$schema"]; !ok {
		doc["$schema"] = "https://opencode.ai/config.json"
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(path, raw)
}

func readJSONObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	// 剥 UTF-8 BOM：CodeBuddy CLI 自写 models.json 带 BOM，直接 Unmarshal
	// 必报 invalid character 'ï'，同步自此外部重写起永久冻结（2026-10-29 事故）。
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}
