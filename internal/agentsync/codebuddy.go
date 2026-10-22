package agentsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CodeBuddy 把平台模型合并进 ~/.codebuddy/models.json（kshell 命名空间）。
type CodeBuddy struct{}

func (CodeBuddy) ToolID() string { return "codebuddy" }

func (CodeBuddy) configPath(home string) string {
	return filepath.Join(home, ".codebuddy", "models.json")
}

func (c CodeBuddy) ConfigExists(home string) bool {
	_, err := os.Stat(c.configPath(home))
	return err == nil
}

func (c CodeBuddy) Sync(home string, in SyncInput) error {
	path := c.configPath(home)
	doc, err := readJSONObject(path)
	if err != nil {
		return err
	}

	models := sliceOfMaps(doc["models"])
	kept := make([]map[string]any, 0, len(models))
	for _, m := range models {
		id, _ := m["id"].(string)
		vendor, _ := m["vendor"].(string)
		if isKshellModelID(id) || vendor == "kshell" {
			continue
		}
		kept = append(kept, m)
	}

	availRaw, _ := doc["availableModels"].([]any)
	availKept := make([]string, 0, len(availRaw))
	for _, a := range availRaw {
		s, _ := a.(string)
		if s == "" || isKshellModelID(s) {
			continue
		}
		availKept = append(availKept, s)
	}

	if in.Enabled {
		for _, p := range in.Platforms {
			base := strings.TrimSpace(p.OpenAIBaseURL)
			if base == "" {
				continue
			}
			url := chatCompletionsURL(base)
			platName := strings.TrimSpace(p.Name)
			if platName == "" {
				platName = p.ID
			}
			for _, mid := range p.Models {
				mid = strings.TrimSpace(mid)
				if mid == "" {
					continue
				}
				id := "kshell-" + p.ID + "/" + mid
				kept = append(kept, map[string]any{
					"id":     id,
					"name":   platName + ": " + mid,
					"vendor": "kshell",
					"apiKey": p.APIKey,
					"url":    url,
				})
				availKept = append(availKept, id)
			}
		}
	}

	doc["models"] = kept
	doc["availableModels"] = availKept
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(path, raw)
}

func isKshellModelID(id string) bool {
	return strings.HasPrefix(id, "kshell-")
}

func chatCompletionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

func sliceOfMaps(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
