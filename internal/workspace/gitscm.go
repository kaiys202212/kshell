package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	errRepoOutside = errors.New("仓库路径越出工作区范围")
	errEmptyCommit = errors.New("提交说明不能为空")
	errEmptyPaths  = errors.New("未指定路径")
	errEmptyRef    = errors.New("分支名不能为空")
)

const gitRemoteTimeout = 60 * time.Second

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

func gitErr(out []byte, err error) error {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return err
	}
	return fmt.Errorf("%s", s)
}

func gitRunAt(root string, timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := gitCmd(ctx, root, args...).CombinedOutput()
	if err != nil {
		return gitErr(out, err)
	}
	return nil
}

func repoPathArgs(wsRoot, repoRel string, paths []string) (abs string, rels []string, err error) {
	abs, err = ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return "", nil, err
	}
	if len(paths) == 0 {
		return "", nil, errEmptyPaths
	}
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
		if p == "" || strings.Contains(p, "..") {
			return "", nil, errRepoOutside
		}
		rels = append(rels, p)
	}
	return abs, rels, nil
}

// Stage 把路径加入 index。
func Stage(wsRoot, repoRel string, paths []string) error {
	abs, rels, err := repoPathArgs(wsRoot, repoRel, paths)
	if err != nil {
		return err
	}
	args := append([]string{"add", "--"}, rels...)
	return gitRunAt(abs, gitStatusTimeout, args...)
}

// Unstage 从 index 撤出（保留工作区）。
func Unstage(wsRoot, repoRel string, paths []string) error {
	abs, rels, err := repoPathArgs(wsRoot, repoRel, paths)
	if err != nil {
		return err
	}
	args := append([]string{"restore", "--staged", "--"}, rels...)
	return gitRunAt(abs, gitStatusTimeout, args...)
}

// Discard 丢弃工作区改动：已跟踪 restore --worktree；未跟踪删除文件。
func Discard(wsRoot, repoRel string, paths []string) error {
	abs, rels, err := repoPathArgs(wsRoot, repoRel, paths)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		tracked := gitRunAt(abs, gitStatusTimeout, "ls-files", "--error-unmatch", "--", rel) == nil
		if tracked {
			if err := gitRunAt(abs, gitStatusTimeout, "restore", "--worktree", "--", rel); err != nil {
				return err
			}
			continue
		}
		full := filepath.Join(abs, filepath.FromSlash(rel))
		full = filepath.Clean(full)
		relToRepo, rerr := filepath.Rel(abs, full)
		if rerr != nil || relToRepo == ".." || strings.HasPrefix(relToRepo, ".."+string(os.PathSeparator)) {
			return errRepoOutside
		}
		if err := os.Remove(full); err != nil {
			return err
		}
	}
	return nil
}

// Commit 在所选仓库提交已暂存内容。
func Commit(wsRoot, repoRel, message string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errEmptyCommit
	}
	return gitRunAt(abs, gitStatusTimeout, "commit", "-m", message)
}

func validRef(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errEmptyRef
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, " \t\\") {
		return errEmptyRef
	}
	return nil
}

// Branches 列出本地分支短名。
func Branches(wsRoot, repoRel string) ([]string, error) {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, "branch", "--format=%(refname:short)").Output()
	if err != nil {
		return nil, gitErr(out, err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// Checkout 切换已有本地分支。
func Checkout(wsRoot, repoRel, name string) error {
	if err := validRef(name); err != nil {
		return err
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitStatusTimeout, "switch", "--", name)
}

// CreateBranch 新建并切换到该分支。
func CreateBranch(wsRoot, repoRel, name string) error {
	if err := validRef(name); err != nil {
		return err
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitStatusTimeout, "switch", "-c", name)
}

// Fetch / Pull / Push 走系统 git，无 --force。
func Fetch(wsRoot, repoRel string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitRemoteTimeout, "fetch")
}

func Pull(wsRoot, repoRel string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitRemoteTimeout, "pull")
}

func Push(wsRoot, repoRel string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitRemoteTimeout, "push")
}

func stashRef(index int) string {
	return fmt.Sprintf("stash@{%d}", index)
}

func StashPush(wsRoot, repoRel, message string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	args := []string{"stash", "push"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "-m", message)
	}
	return gitRunAt(abs, gitStatusTimeout, args...)
}

func StashPop(wsRoot, repoRel string, index int) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitStatusTimeout, "stash", "pop", stashRef(index))
}

func StashApply(wsRoot, repoRel string, index int) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitStatusTimeout, "stash", "apply", stashRef(index))
}

func StashDrop(wsRoot, repoRel string, index int) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitStatusTimeout, "stash", "drop", stashRef(index))
}
