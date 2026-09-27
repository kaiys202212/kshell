package scanners

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yangk/kshell/internal/remote"
)

// DocsScanner 从 README / CLAUDE.md 等文档里找 user@host，并把工作区 .ssh 下的私钥文件名作为候选密钥。
// 只记录密钥**路径**，绝不读取密钥内容。
type DocsScanner struct{}

var docsHostRe = regexp.MustCompile(`([A-Za-z0-9_.\-]+)@([A-Za-z0-9][A-Za-z0-9\-]*\.[A-Za-z0-9\-.]*\.[A-Za-z]{2,}|[A-Za-z0-9][A-Za-z0-9\-]*\.[A-Za-z]{2,})`)

// 邮箱域名：文档里的联系方式会被 user@host 正则命中，必须挡掉。
var emailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "qq.com": true, "foxmail.com": true,
	"163.com": true, "126.com": true, "outlook.com": true, "hotmail.com": true,
	"live.com": true, "sina.com": true, "sohu.com": true, "yahoo.com": true,
	"icloud.com": true, "example.com": true, "example.org": true, "test.com": true,
}

func (DocsScanner) ID() string { return "docs" }

func (DocsScanner) Match(relPath string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(relPath)))
	if base == "claude.md" || base == "agents.md" || base == ".kshell.yaml" || base == ".kshell.yml" {
		return true
	}
	return strings.HasPrefix(base, "readme") && strings.HasSuffix(base, ".md")
}

func (DocsScanner) Extract(root, absPath string) ([]remote.Candidate, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	identity := firstWorkspaceKey(root)

	var out []remote.Candidate
	seen := map[string]bool{}

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		for _, m := range docsHostRe.FindAllStringSubmatch(line, -1) {
			user, host := m[1], m[2]
			// 排除邮箱地址
			if emailDomains[strings.ToLower(host)] {
				continue
			}
			if seen[user+"@"+host] {
				continue
			}
			seen[user+"@"+host] = true

			out = append(out, remote.Candidate{
				Name:         user + "@" + host,
				Host:         host,
				User:         user,
				Port:         22,
				IdentityFile: identity,
				Confidence:   "low",
				Source:       "docs",
				SourceFile:   absPath,
				SourceLine:   lineNo,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// firstWorkspaceKey 只取私钥文件名（不读内容），作为候选 identity。
func firstWorkspaceKey(root string) string {
	sshDir := filepath.Join(root, ".ssh")
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "id_") && !strings.HasSuffix(name, ".pub") {
			return filepath.Join(sshDir, name) // 只返回路径
		}
	}
	return ""
}
