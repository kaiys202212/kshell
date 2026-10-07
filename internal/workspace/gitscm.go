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
	snap.SyncRemote = resolveSyncRemote(abs, snap.Branch, syncRemote, snap.Remotes)
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

// hasRemote 判断 remote 名是否存在于列表中。
func hasRemote(remotes []string, name string) bool {
	for _, r := range remotes {
		if r == name {
			return true
		}
	}
	return false
}

// resolveSyncRemote 解析生效同步源：显式指定且存在则用之；
// 否则 origin → 分支 upstream 所属 remote → 第一个 remote；无 remote 返回空串。
// want 失效（remote 已删）时回退默认，保证前端记忆过期不会算错差异。
// branch 为调用方已知的当前分支（SCMStatus 已读过），避免重复起进程。
func resolveSyncRemote(root, branch, want string, remotes []string) string {
	if len(remotes) == 0 {
		return ""
	}
	if want = strings.TrimSpace(want); want != "" && hasRemote(remotes, want) {
		return want
	}
	if hasRemote(remotes, "origin") {
		return "origin"
	}
	// 无 origin：跟随分支 upstream 所属 remote（值 "." 表示本仓，跳过）
	if branch == "" {
		branch = gitBranch(root)
	}
	if branch != "" && branch != "HEAD" {
		if up := gitConfigVal(root, "branch."+branch+".remote"); up != "" && up != "." && hasRemote(remotes, up) {
			return up
		}
	}
	return remotes[0]
}

// gitConfigVal 读单个 git config 值；读不到返回空串。
func gitConfigVal(root, key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, root, "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// syncBranchName 解析同步用的远端分支名：分支 upstream 与所选源同源时用
// branch.<b>.merge 去掉 refs/heads/ 前缀的真名（支持 `checkout -b x origin/y`
// 这类重命名跟踪），否则按同名回退。
func syncBranchName(root, branch, remote string) string {
	if gitConfigVal(root, "branch."+branch+".remote") == remote {
		if merge := strings.TrimPrefix(gitConfigVal(root, "branch."+branch+".merge"), "refs/heads/"); merge != "" {
			return merge
		}
	}
	return branch
}

// remoteRefExists 校验所选源的远端分支跟踪引用是否已存在：与 scmSyncDiff/HasUpstream
// 同口径（本地校验零网络开销），读不到引用即视为「该源没有这个分支」。
func remoteRefExists(root, remote, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	_, err := gitCmd(ctx, root, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch).Output()
	return err == nil
}

// syncTarget 解析所选源的同步目标：remote 口径同 pickSyncRemote（空串默认解析、
// 显式无效报错），远端分支名在 upstream 同源时用真名，否则按同名回退，
// 并一并返回本地分支名（避免调用方再读一次、两次读之间分支变化造成 refspec 错配）。
// 任一错误统一返回 ("", "", "", err)，调用方无需区分错误来源。
func syncTarget(root, want string) (local, remote, remoteBranch string, err error) {
	// 先解析 remote（显式无效源的报错比「不在分支上」更贴用户意图），branch 顺带算出
	local = gitBranch(root)
	remote, err = pickSyncRemote(root, local, want)
	if err != nil {
		return "", "", "", err
	}
	if remote == "" {
		return "", "", "", errNoSyncRemote
	}
	if local == "" || local == "HEAD" {
		return "", "", "", errNoBranch
	}
	return local, remote, syncBranchName(root, local, remote), nil
}

// scmSyncDiff 计算 HEAD 与所选同步源远端分支的 ahead/behind。
// 远端分支名按 upstream 真名解析（异名跟踪），读不到配置时按同名回退；
// 引用不存在（或不在分支上）时 has=false，面板据此禁用同步。
func scmSyncDiff(root, remote, branch string) (has bool, ahead, behind int) {
	if remote == "" || branch == "" || branch == "HEAD" {
		return false, 0, 0
	}
	remoteBranch := syncBranchName(root, branch, remote)
	if !remoteRefExists(root, remote, remoteBranch) {
		return false, 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	ref := "refs/remotes/" + remote + "/" + remoteBranch
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
// branch 为调用方已知的当前分支，仅无 origin 默认解析时用到。
func pickSyncRemote(root, branch, want string) (string, error) {
	remotes := listRemotes(root)
	if w := strings.TrimSpace(want); w != "" {
		if !hasRemote(remotes, w) {
			return "", fmt.Errorf("同步源 %q 不存在", w)
		}
		return w, nil
	}
	return resolveSyncRemote(root, branch, "", remotes), nil
}

// Fetch 拉取所选同步源（空则按默认解析）；无 remote 时保持裸 fetch 的空操作口径。
// 不带 --force、不带 -u（不改 git config）。
func Fetch(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	r, err := pickSyncRemote(abs, gitBranch(abs), remote)
	if err != nil {
		return err
	}
	if r == "" {
		return gitRunAt(abs, gitRemoteTimeout, "fetch")
	}
	return gitRunAt(abs, gitRemoteTimeout, "fetch", r)
}

// Pull 拉取所选源的同步目标分支；远端分支名按 upstream 真名解析，
// 不再依赖「同名」假设（否则异名跟踪会报 couldn't find remote ref）。
func Pull(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	_, r, rb, err := syncTarget(abs, remote)
	if err != nil {
		return err
	}
	return gitRunAt(abs, gitRemoteTimeout, "pull", r, rb)
}

// Push 推送当前分支到所选源的同步目标分支；显式写出 本地:远端，
// 异名跟踪时更新远端真名。推送前校验该源已有目标分支引用：
// 不存在则报错取消（旧行为是 git fatal；静默新建远端分支是未记录的写副作用）。
func Push(wsRoot, repoRel, remote string) error {
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	local, r, rb, err := syncTarget(abs, remote)
	if err != nil {
		return err
	}
	if !remoteRefExists(abs, r, rb) {
		return fmt.Errorf("同步源 %s 上没有分支 %s，已取消推送", r, rb)
	}
	return gitRunAt(abs, gitRemoteTimeout, "push", r, local+":"+rb)
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
