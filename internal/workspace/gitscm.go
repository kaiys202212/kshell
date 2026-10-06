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
	errRepoOutside  = errors.New("仓库路径越出工作区范围")
	errEmptyCommit  = errors.New("提交说明不能为空")
	errEmptyPaths   = errors.New("未指定路径")
	errEmptyRef     = errors.New("分支名不能为空")
	errNoSyncRemote = errors.New("无可用同步源")
	errNoBranch     = errors.New("不在本地分支上")
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
	IsRepo  bool
	RepoRel string
	Branch  string
	Remotes []string // 仓库全部 remote（git remote 输出；无 remote 为 nil）
	// SyncRemote 生效同步源：入参有效则用之，否则 origin → upstream 所属 remote → 第一个；
	// 无 remote 为空串。差异计算与 fetch/pull/push 均以它为准。
	SyncRemote string
	// HasUpstream 所选同步源上存在同名远端分支引用（refs/remotes/<源>/<分支>），
	// 决定 ↑↓ 徽标与「同步」按钮是否可用（字段名沿用，语义已从 @{upstream} 收窄）。
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

// SCMStatus 读取所选仓库的分组状态、分支、stash、按同步源的 ahead/behind。
func SCMStatus(wsRoot, repoRel, syncRemote string) (SCMSnapshot, error) {
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
	snap.Remotes = listRemotes(abs)
	snap.SyncRemote = resolveSyncRemote(abs, syncRemote, snap.Remotes)
	snap.HasUpstream, snap.Ahead, snap.Behind = scmSyncDiff(abs, snap.SyncRemote, snap.Branch)
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

// listRemotes 列出仓库 remote 名；非仓库/无 remote 返回 nil。
func listRemotes(root string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, root, "remote").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names
}

// resolveSyncRemote 解析生效同步源：显式指定且存在则用之；
// 否则 origin → 分支 upstream 所属 remote → 第一个 remote；无 remote 返回空串。
// want 失效（remote 已删）时回退默认，保证前端记忆过期不会算错差异。
func resolveSyncRemote(root, want string, remotes []string) string {
	if len(remotes) == 0 {
		return ""
	}
	has := func(name string) bool {
		for _, r := range remotes {
			if r == name {
				return true
			}
		}
		return false
	}
	if want = strings.TrimSpace(want); want != "" && has(want) {
		return want
	}
	if has("origin") {
		return "origin"
	}
	// 无 origin：跟随分支 upstream 所属 remote（值 "." 表示本仓，跳过）
	if branch := gitBranch(root); branch != "" && branch != "HEAD" {
		ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
		defer cancel()
		out, err := gitCmd(ctx, root, "config", "--get", "branch."+branch+".remote").Output()
		if err == nil {
			if up := strings.TrimSpace(string(out)); up != "" && up != "." && has(up) {
				return up
			}
		}
	}
	return remotes[0]
}

// scmSyncDiff 计算 HEAD 与所选同步源远端分支的 ahead/behind。
// 远端分支引用不存在（或不在分支上）时 has=false，面板据此禁用同步。
func scmSyncDiff(root, remote, branch string) (has bool, ahead, behind int) {
	if remote == "" || branch == "" || branch == "HEAD" {
		return false, 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	ref := "refs/remotes/" + remote + "/" + branch
	if _, err := gitCmd(ctx, root, "rev-parse", "--verify", "--quiet", ref).Output(); err != nil {
		return false, 0, 0
	}
	out, err := gitCmd(ctx, root, "rev-list", "--left-right", "--count", ref+"...HEAD").Output()
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

// Checkout 切换分支：本地短名走 switch；远端 origin/foo 建跟踪或切到已有本地 foo。
func Checkout(wsRoot, repoRel, name string) error {
	name = strings.TrimSpace(name)
	if err := validRef(name); err != nil {
		return err
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	if local, ok := trackingLocalName(abs, name); ok {
		if gitRunAt(abs, gitStatusTimeout, "show-ref", "--verify", "--quiet", "refs/heads/"+local) == nil {
			return gitRunAt(abs, gitStatusTimeout, "switch", "--", local)
		}
		return gitRunAt(abs, gitStatusTimeout, "switch", "--track", "--", name)
	}
	return gitRunAt(abs, gitStatusTimeout, "switch", "--", name)
}

func trackingLocalName(abs, name string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, "remote").Output()
	if err != nil {
		return "", false
	}
	for _, remote := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		remote = strings.TrimSpace(remote)
		if remote == "" {
			continue
		}
		prefix := remote + "/"
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return strings.TrimPrefix(name, prefix), true
		}
	}
	return "", false
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

// pickSyncRemote 为 fetch/pull/push 选出生效 remote：显式指定必须存在，
// 否则宁可报错也不静默改推别的源；空则按默认规则解析（无 remote 返回空串）。
func pickSyncRemote(root, want string) (string, error) {
	remotes := listRemotes(root)
	if w := strings.TrimSpace(want); w != "" {
		for _, r := range remotes {
			if r == w {
				return w, nil
			}
		}
		return "", fmt.Errorf("同步源 %q 不存在", w)
	}
	return resolveSyncRemote(root, "", remotes), nil
}

// Fetch 拉取所选同步源（空则按默认解析）；无 remote 时保持裸 fetch 的空操作口径。
// 不带 --force、不带 -u（不改 git config）。
func Fetch(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	r, err := pickSyncRemote(abs, remote)
	if err != nil {
		return err
	}
	if r == "" {
		return gitRunAt(abs, gitRemoteTimeout, "fetch")
	}
	return gitRunAt(abs, gitRemoteTimeout, "fetch", r)
}

// Pull 拉取所选源上的当前分支；显式带 remote+branch，不依赖 upstream
// （否则 upstream 指向别的 remote 时会拉错源）。
func Pull(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	r, err := pickSyncRemote(abs, remote)
	if err != nil {
		return err
	}
	if r == "" {
		return errNoSyncRemote
	}
	branch := gitBranch(abs)
	if branch == "" || branch == "HEAD" {
		return errNoBranch
	}
	return gitRunAt(abs, gitRemoteTimeout, "pull", r, branch)
}

// Push 推送当前分支到所选同步源。
func Push(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	r, err := pickSyncRemote(abs, remote)
	if err != nil {
		return err
	}
	if r == "" {
		return errNoSyncRemote
	}
	branch := gitBranch(abs)
	if branch == "" || branch == "HEAD" {
		return errNoBranch
	}
	return gitRunAt(abs, gitRemoteTimeout, "push", r, branch)
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
