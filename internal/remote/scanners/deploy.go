package scanners

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/remote"
)

// DeployScanner 从部署类文件里挖目标主机：compose、Makefile、deploy 脚本、ansible inventory、package.json。
// 噪音偏高，所以一律标 low 置信度，且必须由用户勾选才入库。
type DeployScanner struct{}

var (
	dockerHostRe = regexp.MustCompile(`DOCKER_HOST\s*[:=]\s*"?ssh://([\w.\-]+)@([\w.\-]+)(?::(\d+))?`)
	ansibleHost  = regexp.MustCompile(`ansible_host\s*=\s*(\S+)`)
	ansibleUser  = regexp.MustCompile(`ansible_user\s*=\s*(\S+)`)
	ansiblePort  = regexp.MustCompile(`ansible_port\s*=\s*(\S+)`)
)

var deployFileNames = map[string]bool{
	"docker-compose.yml": true, "docker-compose.yaml": true,
	"makefile": true, "inventory": true, "hosts": true, "package.json": true,
}

func (DeployScanner) ID() string { return "deploy" }

func (DeployScanner) Match(relPath string) bool {
	clean := filepath.ToSlash(relPath)
	base := filepath.Base(clean)
	if deployFileNames[strings.ToLower(base)] {
		return true
	}
	if strings.HasPrefix(strings.ToLower(base), "deploy") {
		switch strings.ToLower(filepath.Ext(base)) {
		case ".sh", ".yml", ".yaml", ".ps1", ".bash":
			return true
		}
	}
	return false
}

func (DeployScanner) Extract(root, absPath string) ([]remote.Candidate, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []remote.Candidate
	seen := map[string]bool{}
	add := func(host, user string, port, line int) {
		if host == "" || seen[user+"@"+host] {
			return
		}
		seen[user+"@"+host] = true
		if port <= 0 {
			port = 22
		}
		out = append(out, remote.Candidate{
			Name:       user + "@" + host,
			Host:       host,
			User:       user,
			Port:       port,
			Confidence: "low",
			Source:     "deploy",
			SourceFile: absPath,
			SourceLine: line,
		})
	}

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if m := dockerHostRe.FindStringSubmatch(line); m != nil {
			port := 22
			if m[3] != "" {
				if p, err := strconv.Atoi(m[3]); err == nil {
					port = p
				}
			}
			add(m[2], m[1], port, lineNo)
			continue
		}

		if m := ansibleHost.FindStringSubmatch(line); m != nil {
			user := ""
			if mu := ansibleUser.FindStringSubmatch(line); mu != nil {
				user = mu[1]
			}
			port := 22
			if mp := ansiblePort.FindStringSubmatch(line); mp != nil {
				if p, err := strconv.Atoi(mp[1]); err == nil {
					port = p
				}
			}
			add(m[1], user, port, lineNo)
			continue
		}

		// 命令行里的 ssh：ssh -p 2200 ops@10.0.0.3 "..."
		fields := strings.Fields(line)
		for i, field := range fields {
			if filepath.Base(strings.TrimSuffix(field, ":")) != "ssh" {
				continue
			}
			port := 22
			for j := i + 1; j < len(fields); j++ {
				token := fields[j]
				if token == "-p" || token == "-P" {
					if j+1 < len(fields) {
						if p, err := strconv.Atoi(fields[j+1]); err == nil {
							port = p
						}
					}
					continue
				}
				if strings.HasPrefix(token, "-") {
					continue
				}
				if strings.Contains(token, "@") {
					parts := strings.SplitN(token, "@", 2)
					add(parts[1], parts[0], port, lineNo)
					break
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
