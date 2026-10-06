package workspace

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

const gitLogDefaultLimit = 200
const gitLogMaxLimit = 500

// LogCommit 一条提交（供 SCM Graph）。
type LogCommit struct {
	Hash         string
	Parents      []string
	Author       string
	Email        string
	Date         string
	Subject      string
	Decorations  []string
}

// GitRef 本地或远端分支。
type GitRef struct {
	Name    string
	Short   string
	Kind    string // local | remote
	Current bool
}

// CommitStat 单次提交的 shortstat。
type CommitStat struct {
	Files      int
	Insertions int
	Deletions  int
}

// Log 读取提交历史。mode: current | all | ref（此时 ref 为分支/远端名）。
func Log(wsRoot, repoRel, mode, ref string, limit int) ([]LogCommit, error) {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = gitLogDefaultLimit
	}
	if limit > gitLogMaxLimit {
		limit = gitLogMaxLimit
	}
	mode = strings.TrimSpace(strings.ToLower(mode))
	args := []string{
		"log",
		"-n", strconv.Itoa(limit),
		"--topo-order",
		"--decorate=short",
		"--pretty=format:%H%x00%P%x00%an%x00%ae%x00%aI%x00%s%x00%D",
	}
	switch mode {
	case "", "current":
		// HEAD
	case "all":
		args = append(args, "--all")
	case "ref":
		if err := validRef(ref); err != nil {
			return nil, err
		}
		// 修订放在选项后：只走该 tip 可达历史（不含 --all 的其它分支 tip）
		args = append(args, ref, "--")
	default:
		return nil, fmt.Errorf("未知 log 模式 %q", mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, args...).Output()
	if err != nil {
		return nil, gitErr(out, err)
	}
	return parseLog(out), nil
}

func parseLog(out []byte) []LogCommit {
	out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
	var commits []LogCommit
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		parts := strings.Split(string(line), "\x00")
		if len(parts) < 7 {
			continue
		}
		var parents []string
		for _, p := range strings.Fields(parts[1]) {
			parents = append(parents, p)
		}
		commits = append(commits, LogCommit{
			Hash:        parts[0],
			Parents:     parents,
			Author:      parts[2],
			Email:       parts[3],
			Date:        parts[4],
			Subject:     parts[5],
			Decorations: parseDecorations(parts[6]),
		})
	}
	return commits
}

func parseDecorations(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(p, "HEAD -> ")
		p = strings.TrimPrefix(p, "tag: ")
		if p == "HEAD" || p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Refs 列出本地与远端分支（不含 remote HEAD 指针）。
func Refs(wsRoot, repoRel string) ([]GitRef, error) {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, "for-each-ref",
		"--format=%(refname)%00%(refname:short)%00%(HEAD)",
		"refs/heads", "refs/remotes").Output()
	if err != nil {
		return nil, gitErr(out, err)
	}
	var refs []GitRef
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 3 {
			continue
		}
		full, short, head := parts[0], parts[1], parts[2]
		if strings.HasSuffix(full, "/HEAD") {
			continue
		}
		kind := "local"
		if strings.HasPrefix(full, "refs/remotes/") {
			kind = "remote"
		}
		refs = append(refs, GitRef{
			Name:    short,
			Short:   short,
			Kind:    kind,
			Current: head == "*",
		})
	}
	return refs, nil
}

// FetchAll 抓取全部远程。
func FetchAll(wsRoot, repoRel string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitRemoteTimeout, "fetch", "--all")
}

func validCommitHash(hash string) error {
	hash = strings.TrimSpace(strings.ToLower(hash))
	if len(hash) < 7 || len(hash) > 40 {
		return errEmptyRef
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errEmptyRef
		}
	}
	return nil
}

// CommitStatAt 读取一次提交的文件/增删行数。
func CommitStatAt(wsRoot, repoRel, hash string) (CommitStat, error) {
	if err := validCommitHash(hash); err != nil {
		return CommitStat{}, err
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return CommitStat{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, "show", "--format=", "--shortstat", hash).Output()
	if err != nil {
		return CommitStat{}, gitErr(out, err)
	}
	return parseShortstat(string(out)), nil
}

func parseShortstat(raw string) CommitStat {
	var st CommitStat
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "changed") {
			continue
		}
		st.Files = leadingInt(line)
		if i := strings.Index(line, "insertion"); i >= 0 {
			st.Insertions = trailingIntBefore(line[:i])
		}
		if i := strings.Index(line, "deletion"); i >= 0 {
			st.Deletions = trailingIntBefore(line[:i])
		}
	}
	return st
}

func leadingInt(s string) int {
	n := 0
	seen := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			seen = true
			n = n*10 + int(r-'0')
			continue
		}
		if seen {
			break
		}
	}
	return n
}

func trailingIntBefore(s string) int {
	s = strings.TrimRightFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	i := strings.LastIndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if i >= 0 {
		s = s[i+1:]
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
