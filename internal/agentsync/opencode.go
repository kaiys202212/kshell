package agentsync

import (
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
				models[m] = map[string]any{"name": m}
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
