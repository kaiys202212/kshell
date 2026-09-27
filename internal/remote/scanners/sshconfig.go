package scanners

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/remote"
)

// SSHConfigScanner 解析 OpenSSH 配置：~/.ssh/config 或工程内的 ssh_config。
// 这是可靠度最高的来源，命中即可直接用。
type SSHConfigScanner struct{}

func (SSHConfigScanner) ID() string { return "sshconfig" }

func (SSHConfigScanner) Match(relPath string) bool {
	clean := filepath.ToSlash(relPath)
	base := filepath.Base(clean)
	if base == "ssh_config" {
		return true
	}
	dir := filepath.ToSlash(filepath.Dir(clean))
	return (dir == ".ssh" || strings.HasSuffix(dir, "/.ssh")) && base == "config"
}

func (SSHConfigScanner) Extract(root, absPath string) ([]remote.Candidate, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cands := ParseSSHConfig(f)
	for i := range cands {
		cands[i].Source = "sshconfig"
		cands[i].SourceFile = absPath
	}
	return cands, nil
}

// ParseSSHConfig 解析 ssh 配置文本；忽略 Host * 这类通配段与注释。
func ParseSSHConfig(r io.Reader) []remote.Candidate {
	var out []remote.Candidate

	var current []string // 当前 Host 行可能声明多个别名
	var host, user, identity string
	var port, hostLine int
	inHost := false

	flush := func() {
		if !inHost {
			return
		}
		for _, name := range current {
			if name == "" || strings.ContainsAny(name, "*?") {
				continue // 通配段不是一条具体连接
			}
			target := host
			if target == "" {
				target = name
			}
			out = append(out, remote.Candidate{
				Name:         name,
				Host:         target,
				User:         user,
				Port:         port,
				IdentityFile: identity,
				Confidence:   "high",
				Source:       "sshconfig",
				SourceLine:   hostLine,
			})
		}
		current, host, user, identity, port, hostLine, inHost = nil, "", "", "", 0, 0, false
	}

	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		value := strings.Join(fields[1:], " ")

		switch key {
		case "host":
			flush()
			inHost = true
			current = strings.Fields(value)
			port = 22
			hostLine = lineNo
		case "hostname":
			host = value
		case "user":
			user = value
		case "port":
			if p, err := strconv.Atoi(value); err == nil {
				port = p
			}
		case "identityfile":
			identity = expandHome(value)
		}
	}
	flush()

	return out
}

func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
