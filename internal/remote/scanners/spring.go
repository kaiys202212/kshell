package scanners

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yangk/kshell/internal/remote"
)

// SpringScanner 从 application*.yml/yaml/properties 里找 ssh/部署相关的 host 配置。
// 字段名各家自定，用「同一层级同时出现 host 与 user/port/key」的启发式判定，
// 并显式排除 datasource/redis/kafka 这类明显不是 ssh 的段。
type SpringScanner struct{}

var springExcludePrefixes = []string{
	"datasource", "redis", "kafka", "mongo", "mongodb", "elasticsearch", "rabbitmq",
	"mail", "ldap", "nacos", "eureka", "consul", "zookeeper", "database",
}

func (SpringScanner) ID() string { return "spring" }

func (SpringScanner) Match(relPath string) bool {
	base := filepath.Base(filepath.ToSlash(relPath))
	if !strings.HasPrefix(base, "application") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".yml", ".yaml", ".properties":
		return true
	}
	return false
}

func (SpringScanner) Extract(root, absPath string) ([]remote.Candidate, error) {
	flat, err := flattenConfig(absPath)
	if err != nil {
		return nil, err
	}

	var out []remote.Candidate
	for key, value := range flat {
		leaf := lastSegment(key)
		if !isHostKey(leaf) {
			continue
		}
		prefix := strings.TrimSuffix(key, "."+leaf)
		if prefix == "" || excludedPrefix(prefix) {
			continue
		}

		user := sibling(flat, prefix, "user", "username", "login", "account")
		portRaw := sibling(flat, prefix, "port", "sshport", "ssh_port")
		identity := sibling(flat, prefix, "key", "keyfile", "key_path", "keypath", "identity", "identityfile", "privatekey", "private_key")

		// 只有 host 一个字段不足以认定这是一条 ssh 连接，避免误报
		if user == "" && portRaw == "" && identity == "" {
			continue
		}

		port := 22
		if p, err := strconv.Atoi(portRaw); err == nil && p > 0 {
			port = p
		}

		out = append(out, remote.Candidate{
			Name:         prefix,
			Host:         strings.TrimSpace(value),
			User:         user,
			Port:         port,
			IdentityFile: expandHome(identity),
			Confidence:   "medium",
			Source:       "spring",
			SourceFile:   absPath,
		})
	}
	return out, nil
}

func sibling(flat map[string]string, prefix string, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(flat[prefix+"."+n]); v != "" {
			return v
		}
	}
	return ""
}

func isHostKey(leaf string) bool {
	switch strings.ToLower(leaf) {
	case "host", "hostname", "hostaddr", "host_addr", "ip", "address", "addr":
		return true
	}
	return false
}

func lastSegment(key string) string {
	if i := strings.LastIndex(key, "."); i >= 0 {
		return key[i+1:]
	}
	return key
}

func excludedPrefix(prefix string) bool {
	lower := strings.ToLower(prefix)
	for _, bad := range springExcludePrefixes {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

// flattenConfig 把 yaml / properties 都摊平成 a.b.c=value，后续逻辑共用一套。
func flattenConfig(path string) (map[string]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".properties" {
		return flattenProperties(path)
	}
	return flattenYAML(path)
}

func flattenProperties(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			out[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	return out, scanner.Err()
}

func flattenYAML(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var node any
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}

	out := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch value := v.(type) {
		case map[string]any:
			for k, sub := range value {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				walk(key, sub)
			}
		case map[any]any:
			for k, sub := range value {
				key := strings.TrimSpace(strLeaf(k))
				if prefix != "" {
					key = prefix + "." + key
				}
				walk(key, sub)
			}
		case []any:
			return // 列表结构不做推断
		default:
			if prefix != "" {
				out[prefix] = strings.TrimSpace(strLeaf(v))
			}
		}
	}
	walk("", node)
	return out, nil
}

func strLeaf(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case int:
		return strconv.Itoa(value)
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}
