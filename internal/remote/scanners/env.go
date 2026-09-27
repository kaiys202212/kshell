package scanners

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/remote"
)

// EnvScanner 解析 .env 里的 SSH_* 变量。字段名各家自定，因此走配置化的键名列表。
type EnvScanner struct{}

var (
	envHostKeys     = []string{"SSH_HOST", "SSH_HOSTNAME", "SSH_HOST_ADDR", "SSH_SERVER", "DEPLOY_HOST"}
	envUserKeys     = []string{"SSH_USER", "SSH_USERNAME", "DEPLOY_USER"}
	envPortKeys     = []string{"SSH_PORT", "DEPLOY_PORT"}
	envIdentityKeys = []string{"SSH_KEY", "SSH_KEY_FILE", "SSH_IDENTITY_FILE", "SSH_IDENTITY", "DEPLOY_KEY"}
	envTargetKeys   = []string{"SSH_TARGET", "DEPLOY_TARGET"}
)

func (EnvScanner) ID() string { return "env" }

func (EnvScanner) Match(relPath string) bool {
	base := filepath.Base(filepath.ToSlash(relPath))
	return base == ".env" || strings.HasPrefix(base, ".env.")
}

func (EnvScanner) Extract(root, absPath string) ([]remote.Candidate, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	lines := map[string]int{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			key := strings.TrimSpace(strings.ToUpper(line[:i]))
			value := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
			values[key] = value
			lines[key] = lineNo
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	host := firstValue(values, envHostKeys)
	hostLine := firstLine(lines, envHostKeys)
	user := firstValue(values, envUserKeys)
	identity := firstValue(values, envIdentityKeys)

	// user@host 合并写法：SSH_TARGET=root@1.2.3.4；SSH_HOST 里也可能直接写 user@host
	if host == "" || user == "" {
		candidates := []string{}
		if target := firstValue(values, envTargetKeys); target != "" {
			candidates = append(candidates, target)
		}
		if strings.Contains(host, "@") {
			candidates = append(candidates, host)
		}
		for _, target := range candidates {
			if !strings.Contains(target, "@") {
				continue
			}
			parts := strings.SplitN(target, "@", 2)
			if user == "" {
				user = parts[0]
			}
			if host == "" || strings.Contains(host, "@") {
				host = parts[1]
			}
		}
	}
	if host == "" {
		return nil, nil
	}

	port := 22
	if raw := firstValue(values, envPortKeys); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil {
			port = p
		}
	}

	name := host
	if user != "" {
		name = user + "@" + host
	}

	return []remote.Candidate{{
		Name:         name,
		Host:         host,
		User:         user,
		Port:         port,
		IdentityFile: expandHome(identity),
		Confidence:   "medium",
		Source:       "env",
		SourceFile:   absPath,
		SourceLine:   hostLine,
	}}, nil
}

func firstValue(values map[string]string, keys []string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(values[k]); v != "" {
			return v
		}
	}
	return ""
}

// firstLine 找到第一个命中键所在行号，供候选详情回溯来源。
func firstLine(lines map[string]int, keys []string) int {
	for _, k := range keys {
		if v, ok := lines[k]; ok && v > 0 {
			return v
		}
	}
	return 0
}
