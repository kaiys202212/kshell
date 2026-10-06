package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errRepoOutside = errors.New("仓库路径越出工作区范围")

// SCMRepo 工作区内一个 git 根（工作区根或嵌套仓）。
type SCMRepo struct {
	Rel    string
	Path   string
	Branch string
}

// SCMEntry 单文件在所选仓库中的 XY 状态。
type SCMEntry struct {
	Path       string
	X, Y       string
	Staged     bool
	Unstaged   bool
	Untracked  bool
	Conflicted bool
}

// SCMStash 一条 stash。
type SCMStash struct {
	Index   int
	Message string
}

// SCMSnapshot 当前选中仓库的 SCM 视图。
type SCMSnapshot struct {
	IsRepo      bool
	RepoRel     string
	Branch      string
	HasUpstream bool
	Ahead       int
	Behind      int
	Repos       []SCMRepo
	Entries     []SCMEntry
	Stashes     []SCMStash
}

// ResolveRepo 把 repoRel 解析成绝对路径，禁止越出工作区。
func ResolveRepo(wsRoot, repoRel string) (string, error) {
	wsRoot = filepath.Clean(strings.TrimSpace(wsRoot))
	repoRel = strings.TrimSpace(strings.ReplaceAll(repoRel, `\`, `/`))
	if repoRel == "" || repoRel == "." {
		return wsRoot, nil
	}
	if strings.Contains(repoRel, "..") {
		return "", errRepoOutside
	}
	abs := filepath.Clean(filepath.Join(wsRoot, filepath.FromSlash(repoRel)))
	rel, err := filepath.Rel(wsRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errRepoOutside
	}
	return abs, nil
}

// ListGitRepos 枚举工作区根仓（若是仓）及嵌套仓。
func ListGitRepos(wsRoot string) []SCMRepo {
	wsRoot = filepath.Clean(wsRoot)
	var out []SCMRepo
	if _, isRepo, _ := gitStatusAt(wsRoot); isRepo {
		out = append(out, SCMRepo{Rel: "", Path: wsRoot, Branch: gitBranch(wsRoot)})
	}
	for _, nested := range nestedGitRoots(wsRoot) {
		rel, err := filepath.Rel(wsRoot, nested)
		if err != nil {
			continue
		}
		relKey := filepath.ToSlash(rel)
		if relKey == "." {
			continue
		}
		out = append(out, SCMRepo{Rel: relKey, Path: nested, Branch: gitBranch(nested)})
	}
	return out
}

// SCMStatus 读取所选仓库的分组状态、分支、stash、ahead/behind。
func SCMStatus(wsRoot, repoRel string) (SCMSnapshot, error) {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return SCMSnapshot{}, err
	}
	if repoRel == "." {
		repoRel = ""
	}
	snap := SCMSnapshot{RepoRel: repoRel, Repos: ListGitRepos(wsRoot)}
	entries, isRepo, err := scmPorcelain(abs)
	if err != nil {
		return SCMSnapshot{}, err
	}
	snap.IsRepo = isRepo
	if !isRepo {
		return snap, nil
	}
	snap.Branch = gitBranch(abs)
	snap.Entries = entries
	snap.HasUpstream, snap.Ahead, snap.Behind = scmAheadBehind(abs)
	snap.Stashes = scmStashes(abs)
	return snap, nil
}

func scmPorcelain(root string) ([]SCMEntry, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	topOut, execErr := gitCmd(ctx, root, "rev-parse", "--show-toplevel").Output()
	if execErr != nil {
		return nil, false, nil
	}
	top := filepath.Clean(strings.TrimSpace(string(topOut)))
	cmd := gitCmd(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, true, ctx.Err()
		}
		return nil, false, nil
	}
	return parseSCMPorcelainZ(out.Bytes(), root, top), true, nil
}

func parseSCMPorcelainZ(data []byte, wsRoot, top string) []SCMEntry {
	var entries []SCMEntry
	rest := data
	for len(rest) > 0 {
		entry := rest
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			entry, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		if len(entry) < 4 {
			continue
		}
		x, y, path := entry[0], entry[1], string(entry[3:])
		if x == 'R' || x == 'C' {
			if i := bytes.IndexByte(rest, 0); i >= 0 {
				rest = rest[i+1:]
			} else {
				rest = nil
			}
		}
		if path == "" || (x == '!' && y == '!') {
			continue
		}
		key := relKey(wsRoot, top, path)
		e := SCMEntry{Path: key, X: string(x), Y: string(y)}
		e.Conflicted = statusFromXY(x, y) == "conflicted"
		e.Untracked = x == '?' && y == '?'
		if !e.Untracked {
			e.Staged = x != ' ' && x != '?'
			e.Unstaged = y != ' ' && y != '?'
		}
		entries = append(entries, e)
	}
	return entries
}

func scmAheadBehind(root string) (has bool, ahead, behind int) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	if _, err := gitCmd(ctx, root, "rev-parse", "--abbrev-ref", "@{upstream}").Output(); err != nil {
		return false, 0, 0
	}
	out, err := gitCmd(ctx, root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD").Output()
	if err != nil {
		return true, 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return true, 0, 0
	}
	behind, _ = strconv.Atoi(parts[0])
	ahead, _ = strconv.Atoi(parts[1])
	return true, ahead, behind
}

func scmStashes(root string) []SCMStash {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, root, "stash", "list").Output()
	if err != nil {
		return nil
	}
	var stashes []SCMStash
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		msg := line
		if j := strings.Index(line, ":"); j >= 0 {
			msg = strings.TrimSpace(line[j+1:])
		}
		stashes = append(stashes, SCMStash{Index: i, Message: msg})
	}
	return stashes
}
