package providers

import (
	"encoding/json"
	"strings"
)

// MergeSettingsArg 把 obj 合入 args 中已有的内联 `--settings` JSON 对象；
// 没有时追加一组。值为文件路径（非 `{` 开头）或非法 JSON 时原样返回——
// 宁可放弃注入也不破坏启动。env 键做键级浅合并：注入键覆盖同名键，其余保留。
func MergeSettingsArg(args []string, obj map[string]any) []string {
	for i := len(args) - 2; i >= 0; i-- {
		if args[i] != "--settings" {
			continue
		}
		v := strings.TrimSpace(args[i+1])
		if !strings.HasPrefix(v, "{") {
			return args
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err != nil || m == nil {
			return args
		}
		for k, val := range obj {
			if em, ok := val.(map[string]any); ok {
				// map 值（如 env）做键级浅合并，保留既有键
				if exist, ok := m[k].(map[string]any); ok {
					merged := make(map[string]any, len(exist)+len(em))
					for ek, ev := range exist {
						merged[ek] = ev
					}
					for ek, ev := range em {
						merged[ek] = ev
					}
					m[k] = merged
					continue
				}
			}
			m[k] = val
		}
		b, err := json.Marshal(m)
		if err != nil {
			return args
		}
		out := append([]string(nil), args...)
		out[i+1] = string(b)
		return out
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return args
	}
	return append(append([]string(nil), args...), "--settings", string(b))
}
