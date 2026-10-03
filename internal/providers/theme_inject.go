package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// MergeEnv 合并两组环境变量，b 覆盖 a；两者皆空返回 nil。不修改入参。
func MergeEnv(a, b map[string]string) map[string]string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// EnvList 把环境变量展开成 "K=V" 列表（按 key 排序保证确定性）；空 map 返回 nil。
func EnvList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// writeThemeJSON 把主题覆盖配置写到 kshell 缓存目录（0600），返回文件路径。
func writeThemeJSON(dir, name string, content any) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
