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
	// claim：旧 kshell 条目裸 mid ∪ 当前平台裸 mid。用于清掉与之碰撞的
	// vendor=user 裸名镜像（/model 里的 Custom），以及平台已删后的孤儿。
	claim := map[string]bool{}
	for _, m := range models {
		id, _ := m["id"].(string)
		vendor, _ := m["vendor"].(string)
		if isKshellModelID(id) || vendor == "kshell" {
			if bare := bareModelID(id); bare != "" {
				claim[bare] = true
			}
		}
	}
	for _, p := range in.Platforms {
		for _, mid := range p.Models {
			if mid = strings.TrimSpace(mid); mid != "" {
				claim[mid] = true
			}
		}
	}

	kept := make([]map[string]any, 0, len(models))
	for _, m := range models {
		id, _ := m["id"].(string)
		vendor, _ := m["vendor"].(string)
		if isKshellModelID(id) || vendor == "kshell" {
			continue
		}
		if claimedModelID(id, claim) {
			continue
		}
		kept = append(kept, m)
	}

	availRaw, _ := doc["availableModels"].([]any)
	availKept := make([]string, 0, len(availRaw))
	for _, a := range availRaw {
		s, _ := a.(string)
		if s == "" || isKshellModelID(s) || claimedModelID(s, claim) {
			continue
		}
		availKept = append(availKept, s)
	}
	// kshell 不再向 availableModels 写入条目：CLI 把该键当权威过滤清单，
	// 写入会挤出内置模型（ACP 下拉丢内置）。清完自有条目后为空则整键删除；
	// 仍有外来条目时保留原样（不越权动别人的清单）。
	if len(availKept) == 0 {
		delete(doc, "availableModels")
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
			}
		}
	}

	if len(availKept) > 0 {
		doc["availableModels"] = availKept
	}

	doc["models"] = kept
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(path, raw); err != nil {
		return err
	}

	// 先落 models.json 再校正 settings.json：models.json 写失败时不动默认模型，
	// 避免两文件状态分叉。
	candidateIDs := make([]string, 0, len(kept))
	for _, m := range kept {
		if id, _ := m["id"].(string); id != "" {
			candidateIDs = append(candidateIDs, id)
		}
	}
	return c.reconcileDefaultModel(filepath.Dir(path), candidateIDs)
}

// reconcileDefaultModel 校正 ~/.codebuddy/settings.json 的默认模型（"model"，
// 由 CodeBuddy CLI 写入，kshell 不管理该文件）：平台被删除后该值悬空，
// 新建实例不切模型即报错。降级顺序：
//  1. 精确命中候选 id → 不动；
//  2. 剥离平台前缀（最后一个 ':' 或 '/' 之后）得裸名，命中候选 id 或其 '/'
//     后缀（如 custom-local:glm-5.3-flash → glm-5.3-flash）→ 回写完整 id；
//  3. 无命中但原值形似命名空间 id（含 ':' 或 '/' 分隔符）且存在 kshell 候选
//     → 回写第一个 kshell id；
//  4. 其余（含纯裸名无命中——可能是 CLI 内置模型 id）→ 不动。
//
// settings.json 缺失/损坏/model 非字符串一律跳过；静默自愈，不外抛告警。
// 注意：回写会按字母序重排 settings.json 的键（JSON 无语义，可接受）。
func (CodeBuddy) reconcileDefaultModel(codebuddyDir string, candidates []string) error {
	settingsPath := filepath.Join(codebuddyDir, "settings.json")
	settings, err := readJSONObject(settingsPath)
	if err != nil {
		return nil // 损坏：跳过，不扩大故障面
	}
	cur, _ := settings["model"].(string)
	cur = strings.TrimSpace(cur)
	if cur == "" || len(candidates) == 0 {
		return nil
	}
	for _, id := range candidates {
		if id == cur {
			return nil
		}
	}
	bare := cur
	qualified := strings.ContainsAny(cur, ":/")
	if i := strings.LastIndexAny(cur, ":/"); i >= 0 && i+1 < len(cur) {
		bare = cur[i+1:]
	}
	resolved := ""
	for _, id := range candidates {
		if id == bare || strings.HasSuffix(id, "/"+bare) {
			resolved = id
			break
		}
	}
	if resolved == "" && qualified {
		// 仅当原值形似已消失的命名空间 id（带平台前缀）才兜底回退；
		// 纯裸名无命中可能是 CLI 内置模型，不得覆盖。
		for _, id := range candidates {
			if isKshellModelID(id) {
				resolved = id
				break
			}
		}
	}
	if resolved == "" || resolved == cur {
		return nil
	}
	settings["model"] = resolved
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(settingsPath, raw)
}

func isKshellModelID(id string) bool {
	return strings.HasPrefix(id, "kshell-")
}

// bareModelID 从 kshell-<plat>/<mid> 或带 :/ 前缀的 id 取裸 mid；无分隔则原样。
func bareModelID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if i := strings.LastIndexAny(id, ":/"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

// claimedModelID 报告 id（或其裸名）是否落在 kshell claim 集合内。
func claimedModelID(id string, claim map[string]bool) bool {
	if claim[id] {
		return true
	}
	return claim[bareModelID(id)]
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
