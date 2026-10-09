package desktop

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	remotefs "github.com/yangk/kshell/internal/remote/fs"
	"github.com/yangk/kshell/internal/workspace"
)

const (
	remoteGitStatusTimeout = 15 * time.Second
	remoteGitRemoteTimeout = 60 * time.Second
)

// remoteGit 经 ssh 在远端执行 git -C，复用 workspace 的输出解析。
type remoteGit struct {
	app *App
	r   *remoteWS
}

func (a *App) remoteGit(r *remoteWS) *remoteGit {
	return &remoteGit{app: a, r: r}
}

func (g *remoteGit) ensureGit(ctx context.Context) error {
	stdout, stderr, err := g.app.remoteRunner()(ctx, g.r.conn, "command -v git", nil)
	if err != nil || strings.TrimSpace(string(stdout)) == "" {
		if len(stderr) > 0 && err != nil {
			return fmt.Errorf("%w: %s", errRemoteGitUnavailable, strings.TrimSpace(string(stderr)))
		}
		return errRemoteGitUnavailable
	}
	return nil
}

func (g *remoteGit) run(ctx context.Context, root string, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	cmd := buildRemoteGitCmd(root, args)
	return g.app.remoteRunner()(ctx, g.r.conn, cmd, stdin)
}

func (g *remoteGit) output(ctx context.Context, root string, args ...string) ([]byte, error) {
	stdout, stderr, err := g.run(ctx, root, nil, args...)
	if err != nil {
		if isGitNotFound(err, stderr) {
			return stdout, errRemoteGitUnavailable
		}
		return stdout, err
	}
	return stdout, nil
}

func (g *remoteGit) runAt(ctx context.Context, root string, args ...string) error {
	stdout, stderr, err := g.run(ctx, root, nil, args...)
	if err != nil {
		if isGitNotFound(err, stderr) {
			return errRemoteGitUnavailable
		}
		out := append(stdout, stderr...)
		return remoteGitErr(out, err)
	}
	return nil
}

func (g *remoteGit) runAtStdin(ctx context.Context, root string, stdin []byte, args ...string) error {
	stdout, stderr, err := g.run(ctx, root, stdin, args...)
	if err != nil {
		if isGitNotFound(err, stderr) {
			return errRemoteGitUnavailable
		}
		return remoteGitErr(append(stdout, stderr...), err)
	}
	return nil
}

func buildRemoteGitCmd(root string, args []string) string {
	var b strings.Builder
	b.WriteString("git -C ")
	b.WriteString(shellSingleQuote(root))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellSingleQuote(a))
	}
	return b.String()
}

func isGitNotFound(err error, stderr []byte) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error() + " " + string(stderr))
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "err.ssh.exit|127")
}

func remoteGitErr(out []byte, err error) error {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return err
	}
	return fmt.Errorf("%s", s)
}

func resolveRemoteRepo(wsRoot, repoRel string) (string, error) {
	abs, err := remotefs.ResolveUnderRoot(wsRoot, repoRel)
	if err != nil {
		return "", errRepoOutsideMapped(err)
	}
	return abs, nil
}

func errRepoOutsideMapped(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "err.remote.path_escape") || strings.Contains(err.Error(), "out_of_workspace") {
		return fmt.Errorf("err.git.out_of_workspace")
	}
	return err
}

func remoteRepoPathArgs(wsRoot, repoRel string, paths []string) (abs string, rels []string, err error) {
	abs, err = resolveRemoteRepo(wsRoot, repoRel)
	if err != nil {
		return "", nil, err
	}
	if len(paths) == 0 {
		return "", nil, fmt.Errorf("err.git.no_paths")
	}
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
		if p == "" || strings.Contains(p, "..") {
			return "", nil, fmt.Errorf("err.git.out_of_workspace")
		}
		rels = append(rels, p)
	}
	return abs, rels, nil
}

func (g *remoteGit) inspect(ctx context.Context) (workspace.GitReport, error) {
	if err := g.ensureGit(ctx); err != nil {
		return workspace.GitReport{}, err
	}
	root := g.r.root
	status, isRepo, err := g.statusAt(ctx, root, true)
	if err != nil {
		return workspace.GitReport{}, err
	}
	rep := workspace.GitReport{Status: status, IsRepo: isRepo, DirBranches: map[string]string{}}
	if isRepo {
		rep.Branch = g.branch(ctx, root)
		if rep.Branch != "" {
			rep.DirBranches[""] = rep.Branch
		}
	}
	for _, nested := range g.nestedRoots(ctx, root) {
		rel, err := posixRelKey(root, nested)
		if err != nil || rel == "." || rel == "" {
			continue
		}
		if br := g.branch(ctx, nested); br != "" {
			rep.DirBranches[rel] = br
		}
		st, nestedRepo, nerr := g.statusAt(ctx, nested, true)
		if nerr != nil || !nestedRepo {
			continue
		}
		if rep.Status == nil {
			rep.Status = map[string]string{}
		}
		prefix := rel + "/"
		for k := range rep.Status {
			if k == rel || strings.HasPrefix(k, prefix) {
				delete(rep.Status, k)
			}
		}
		for k, v := range st {
			rep.Status[rel+"/"+strings.TrimPrefix(k, "./")] = v
		}
	}
	if rep.Status == nil {
		rep.Status = map[string]string{}
	}
	return rep, nil
}

func (g *remoteGit) statusAt(ctx context.Context, root string, withIgnored bool) (map[string]string, bool, error) {
	topOut, err := g.output(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		if err == errRemoteGitUnavailable {
			return nil, false, err
		}
		return nil, false, nil
	}
	top := posixCleanRemote(strings.TrimSpace(string(topOut)))
	args := []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
	if withIgnored {
		args = append(args, "--ignored=matching")
	}
	out, err := g.output(ctx, root, args...)
	if err != nil {
		if err == errRemoteGitUnavailable {
			return nil, false, err
		}
		if ctx.Err() == context.DeadlineExceeded {
			return nil, true, ctx.Err()
		}
		return nil, false, nil
	}
	return workspace.ParsePorcelainZ(out, root, top, true), true, nil
}

func (g *remoteGit) branch(ctx context.Context, root string) string {
	out, err := g.output(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (g *remoteGit) nestedRoots(ctx context.Context, wsRoot string) []string {
	// 跳过常见大目录；打印各嵌套 .git（文件或目录）路径，取其父目录为仓根。
	cmd := fmt.Sprintf(
		`LC_ALL=C find %s -mindepth 2 \( -name node_modules -o -name vendor -o -name dist -o -name build \) -prune -o \( -name .git -print \)`,
		shellSingleQuote(wsRoot),
	)
	stdout, _, err := g.app.remoteRunner()(ctx, g.r.conn, cmd, nil)
	if err != nil {
		return nil
	}
	var roots []string
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parent := path.Dir(posixCleanRemote(line))
		if parent == "" || parent == "/" || parent == wsRoot {
			continue
		}
		roots = append(roots, parent)
	}
	return roots
}

func posixCleanRemote(p string) string {
	if p == "" {
		return "/"
	}
	c := path.Clean(p)
	if c == "." {
		return "/"
	}
	if !strings.HasPrefix(c, "/") {
		c = "/" + c
	}
	return c
}

func posixRelKey(base, target string) (string, error) {
	b := posixCleanRemote(base)
	t := posixCleanRemote(target)
	if b == t {
		return ".", nil
	}
	prefix := b + "/"
	if strings.HasPrefix(t, prefix) {
		return t[len(prefix):], nil
	}
	return "", fmt.Errorf("not under")
}

func (g *remoteGit) scm(ctx context.Context, repoRel, syncRemote string) (workspace.SCMSnapshot, error) {
	if err := g.ensureGit(ctx); err != nil {
		return workspace.SCMSnapshot{}, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return workspace.SCMSnapshot{}, err
	}
	if repoRel == "." {
		repoRel = ""
	}
	snap := workspace.SCMSnapshot{RepoRel: repoRel, Repos: g.listRepos(ctx)}
	entries, isRepo, err := g.scmPorcelain(ctx, abs)
	if err != nil {
		return workspace.SCMSnapshot{}, err
	}
	snap.IsRepo = isRepo
	if !isRepo {
		return snap, nil
	}
	snap.Branch = g.branch(ctx, abs)
	snap.Entries = entries
	snap.Remotes = g.listRemotes(ctx, abs)
	snap.SyncRemote = g.resolveSyncRemote(ctx, abs, snap.Branch, syncRemote, snap.Remotes)
	snap.HasUpstream, snap.Ahead, snap.Behind = g.scmSyncDiff(ctx, abs, snap.SyncRemote, snap.Branch)
	snap.Stashes = g.scmStashes(ctx, abs)
	return snap, nil
}

func (g *remoteGit) listRepos(ctx context.Context) []workspace.SCMRepo {
	root := g.r.root
	var out []workspace.SCMRepo
	if _, isRepo, _ := g.statusAt(ctx, root, false); isRepo {
		out = append(out, workspace.SCMRepo{Rel: "", Path: root, Branch: g.branch(ctx, root)})
	}
	for _, nested := range g.nestedRoots(ctx, root) {
		rel, err := posixRelKey(root, nested)
		if err != nil || rel == "." || rel == "" {
			continue
		}
		out = append(out, workspace.SCMRepo{Rel: rel, Path: nested, Branch: g.branch(ctx, nested)})
	}
	return out
}

func (g *remoteGit) scmPorcelain(ctx context.Context, root string) ([]workspace.SCMEntry, bool, error) {
	topOut, err := g.output(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		if err == errRemoteGitUnavailable {
			return nil, false, err
		}
		return nil, false, nil
	}
	top := posixCleanRemote(strings.TrimSpace(string(topOut)))
	out, err := g.output(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		if err == errRemoteGitUnavailable {
			return nil, false, err
		}
		if ctx.Err() == context.DeadlineExceeded {
			return nil, true, ctx.Err()
		}
		return nil, false, nil
	}
	return workspace.ParseSCMPorcelainZ(out, root, top, true), true, nil
}

func (g *remoteGit) listRemotes(ctx context.Context, root string) []string {
	out, err := g.output(ctx, root, "remote")
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

func (g *remoteGit) gitConfigVal(ctx context.Context, root, key string) string {
	out, err := g.output(ctx, root, "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hasRemoteName(remotes []string, name string) bool {
	for _, r := range remotes {
		if r == name {
			return true
		}
	}
	return false
}

func (g *remoteGit) resolveSyncRemote(ctx context.Context, root, branch, want string, remotes []string) string {
	if len(remotes) == 0 {
		return ""
	}
	if want = strings.TrimSpace(want); want != "" && hasRemoteName(remotes, want) {
		return want
	}
	if hasRemoteName(remotes, "origin") {
		return "origin"
	}
	if branch == "" {
		branch = g.branch(ctx, root)
	}
	if branch != "" && branch != "HEAD" {
		if up := g.gitConfigVal(ctx, root, "branch."+branch+".remote"); up != "" && up != "." && hasRemoteName(remotes, up) {
			return up
		}
	}
	return remotes[0]
}

func (g *remoteGit) syncBranchName(ctx context.Context, root, branch, remote string) string {
	if g.gitConfigVal(ctx, root, "branch."+branch+".remote") == remote {
		if merge := strings.TrimPrefix(g.gitConfigVal(ctx, root, "branch."+branch+".merge"), "refs/heads/"); merge != "" {
			return merge
		}
	}
	return branch
}

func (g *remoteGit) remoteRefExists(ctx context.Context, root, remote, branch string) bool {
	_, err := g.output(ctx, root, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch)
	return err == nil
}

func (g *remoteGit) scmSyncDiff(ctx context.Context, root, remote, branch string) (has bool, ahead, behind int) {
	if remote == "" || branch == "" || branch == "HEAD" {
		return false, 0, 0
	}
	remoteBranch := g.syncBranchName(ctx, root, branch, remote)
	if !g.remoteRefExists(ctx, root, remote, remoteBranch) {
		return false, 0, 0
	}
	ref := "refs/remotes/" + remote + "/" + remoteBranch
	out, err := g.output(ctx, root, "rev-list", "--left-right", "--count", ref+"...HEAD")
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

func (g *remoteGit) scmStashes(ctx context.Context, root string) []workspace.SCMStash {
	out, err := g.output(ctx, root, "stash", "list")
	if err != nil {
		return nil
	}
	var stashes []workspace.SCMStash
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		msg := line
		if j := strings.Index(line, ":"); j >= 0 {
			msg = strings.TrimSpace(line[j+1:])
		}
		stashes = append(stashes, workspace.SCMStash{Index: i, Message: msg})
	}
	return stashes
}

func (g *remoteGit) withTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(g.app.sshCtx(), d)
}

func (g *remoteGit) stage(ctx context.Context, repoRel string, paths []string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, rels, err := remoteRepoPathArgs(g.r.root, repoRel, paths)
	if err != nil {
		return err
	}
	args := append([]string{"add", "--"}, rels...)
	return g.runAt(ctx, abs, args...)
}

func (g *remoteGit) unstage(ctx context.Context, repoRel string, paths []string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, rels, err := remoteRepoPathArgs(g.r.root, repoRel, paths)
	if err != nil {
		return err
	}
	args := append([]string{"restore", "--staged", "--"}, rels...)
	return g.runAt(ctx, abs, args...)
}

func (g *remoteGit) discard(ctx context.Context, repoRel string, paths []string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, rels, err := remoteRepoPathArgs(g.r.root, repoRel, paths)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		tracked := g.runAt(ctx, abs, "ls-files", "--error-unmatch", "--", rel) == nil
		if tracked {
			if err := g.runAt(ctx, abs, "restore", "--worktree", "--", rel); err != nil {
				return err
			}
			continue
		}
		full, err := remotefs.ResolveUnderRoot(abs, rel)
		if err != nil {
			return fmt.Errorf("err.git.out_of_workspace")
		}
		rm := fmt.Sprintf("rm -f -- %s", shellSingleQuote(full))
		if _, stderr, err := g.app.remoteRunner()(ctx, g.r.conn, rm, nil); err != nil {
			if len(stderr) > 0 {
				return fmt.Errorf("%s", strings.TrimSpace(string(stderr)))
			}
			return err
		}
	}
	return nil
}

func (g *remoteGit) commit(ctx context.Context, repoRel, message string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("err.git.empty_commit_msg")
	}
	return g.runAt(ctx, abs, "commit", "-m", message)
}

func (g *remoteGit) fileDiff(ctx context.Context, repoRel, filePath, side string) (workspace.DiffResult, error) {
	if err := g.ensureGit(ctx); err != nil {
		return workspace.DiffResult{}, err
	}
	if side != "working" && side != "staged" {
		return workspace.DiffResult{}, fmt.Errorf("err.git.invalid_diff_side")
	}
	abs, rels, err := remoteRepoPathArgs(g.r.root, repoRel, []string{filePath})
	if err != nil {
		return workspace.DiffResult{}, err
	}
	rel := rels[0]
	tracked := g.runAt(ctx, abs, "ls-files", "--error-unmatch", "--", rel) == nil
	if !tracked {
		if side == "staged" {
			return workspace.DiffResult{}, nil
		}
		return g.untrackedDiff(ctx, abs, rel)
	}
	args := []string{"diff", "--"}
	if side == "staged" {
		args = []string{"diff", "--cached", "--"}
	}
	args = append(args, rel)
	stdout, stderr, err := g.run(ctx, abs, nil, args...)
	out := append(stdout, stderr...)
	if err != nil && len(out) == 0 {
		if isGitNotFound(err, stderr) {
			return workspace.DiffResult{}, errRemoteGitUnavailable
		}
		return workspace.DiffResult{}, remoteGitErr(out, err)
	}
	text := string(out)
	binary := strings.Contains(text, "Binary files ") || strings.Contains(text, "GIT binary patch")
	return workspace.DiffResult{Text: text, Binary: binary}, nil
}

func (g *remoteGit) untrackedDiff(ctx context.Context, root, rel string) (workspace.DiffResult, error) {
	full, err := remotefs.ResolveUnderRoot(root, rel)
	if err != nil {
		return workspace.DiffResult{}, fmt.Errorf("err.git.out_of_workspace")
	}
	data, err := g.r.fs.ReadFile(ctx, root, full, 2*1024*1024)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	if strings.IndexByte(string(data), 0) >= 0 || containsNUL(data) {
		return workspace.DiffResult{Binary: true, Untracked: true}, nil
	}
	norm := strings.ReplaceAll(string(data), "\r\n", "\n")
	norm = strings.TrimSuffix(norm, "\n")
	var body strings.Builder
	n := 0
	if norm != "" || len(data) > 0 {
		for _, line := range strings.Split(norm, "\n") {
			n++
			body.WriteString("+")
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	hunk := "@@ -0,0 +0,0 @@\n"
	if n > 0 {
		hunk = fmt.Sprintf("@@ -0,0 +1,%d @@\n", n)
	}
	text := fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n%s%s",
		rel, rel, rel, hunk, body.String())
	return workspace.DiffResult{Text: text, Untracked: true}, nil
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

func (g *remoteGit) applyHunk(ctx context.Context, repoRel, filePath, side, hunkPatch, op string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	if _, _, err := remoteRepoPathArgs(g.r.root, repoRel, []string{filePath}); err != nil {
		return err
	}
	if !strings.Contains(hunkPatch, "diff --git") {
		return fmt.Errorf("err.git.hunk_missing_header")
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	args := []string{"apply", "--whitespace=nowarn"}
	switch op {
	case "stage":
		args = append(args, "--cached")
	case "unstage":
		args = append(args, "-R", "--cached")
	case "discard":
		args = append(args, "-R")
	default:
		return fmt.Errorf("err.git.invalid_hunk_op")
	}
	_ = side
	return g.runAtStdin(ctx, abs, []byte(hunkPatch), args...)
}

func (g *remoteGit) branches(ctx context.Context, repoRel string) ([]string, error) {
	if err := g.ensureGit(ctx); err != nil {
		return nil, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return nil, err
	}
	out, err := g.output(ctx, abs, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
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

func validRemoteRef(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("err.git.empty_branch_name")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, " \t\\") {
		return fmt.Errorf("err.git.empty_branch_name")
	}
	return nil
}

func (g *remoteGit) trackingLocalName(ctx context.Context, abs, name string) (string, bool) {
	out, err := g.output(ctx, abs, "remote")
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

func (g *remoteGit) checkout(ctx context.Context, repoRel, name string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := validRemoteRef(name); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	if local, ok := g.trackingLocalName(ctx, abs, name); ok {
		if g.runAt(ctx, abs, "show-ref", "--verify", "--quiet", "refs/heads/"+local) == nil {
			return g.runAt(ctx, abs, "switch", "--", local)
		}
		return g.runAt(ctx, abs, "switch", "--track", "--", name)
	}
	return g.runAt(ctx, abs, "switch", "--", name)
}

func (g *remoteGit) createBranch(ctx context.Context, repoRel, name string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	if err := validRemoteRef(name); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "switch", "-c", name)
}

func (g *remoteGit) pickSyncRemote(ctx context.Context, root, branch, want string) (string, error) {
	remotes := g.listRemotes(ctx, root)
	if w := strings.TrimSpace(want); w != "" {
		if !hasRemoteName(remotes, w) {
			return "", fmt.Errorf("err.git.sync_remote_missing|%s", w)
		}
		return w, nil
	}
	return g.resolveSyncRemote(ctx, root, branch, "", remotes), nil
}

func (g *remoteGit) syncTarget(ctx context.Context, root, want string) (local, remote, remoteBranch string, err error) {
	local = g.branch(ctx, root)
	remote, err = g.pickSyncRemote(ctx, root, local, want)
	if err != nil {
		return "", "", "", err
	}
	if remote == "" {
		return "", "", "", fmt.Errorf("err.git.no_sync_source")
	}
	if local == "" || local == "HEAD" {
		return "", "", "", fmt.Errorf("err.git.no_branch")
	}
	return local, remote, g.syncBranchName(ctx, root, local, remote), nil
}

func (g *remoteGit) fetch(ctx context.Context, repoRel, remote string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	r, err := g.pickSyncRemote(ctx, abs, g.branch(ctx, abs), remote)
	if err != nil {
		return err
	}
	if r == "" {
		return g.runAt(ctx, abs, "fetch")
	}
	return g.runAt(ctx, abs, "fetch", r)
}

func (g *remoteGit) pull(ctx context.Context, repoRel, remote string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	_, r, rb, err := g.syncTarget(ctx, abs, remote)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "pull", r, rb)
}

func (g *remoteGit) push(ctx context.Context, repoRel, remote string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	local, r, rb, err := g.syncTarget(ctx, abs, remote)
	if err != nil {
		return err
	}
	if !g.remoteRefExists(ctx, abs, r, rb) {
		return fmt.Errorf("err.git.sync_remote_branch_missing|%s|%s", r, rb)
	}
	return g.runAt(ctx, abs, "push", r, local+":"+rb)
}

func (g *remoteGit) stashPush(ctx context.Context, repoRel, message string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	args := []string{"stash", "push"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "-m", message)
	}
	return g.runAt(ctx, abs, args...)
}

func stashRefRemote(index int) string {
	return fmt.Sprintf("stash@{%d}", index)
}

func (g *remoteGit) stashPop(ctx context.Context, repoRel string, index int) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "stash", "pop", stashRefRemote(index))
}

func (g *remoteGit) stashApply(ctx context.Context, repoRel string, index int) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "stash", "apply", stashRefRemote(index))
}

func (g *remoteGit) stashDrop(ctx context.Context, repoRel string, index int) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "stash", "drop", stashRefRemote(index))
}

func (g *remoteGit) log(ctx context.Context, repoRel, mode, ref string, limit, skip int) ([]workspace.LogCommit, error) {
	if err := g.ensureGit(ctx); err != nil {
		return nil, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	if skip < 0 {
		skip = 0
	}
	mode = strings.TrimSpace(strings.ToLower(mode))
	args := []string{
		"log",
		"-n", strconv.Itoa(limit),
		"--skip", strconv.Itoa(skip),
		"--topo-order",
		"--decorate=short",
		"--pretty=format:%H%x00%P%x00%an%x00%ae%x00%aI%x00%s%x00%D",
	}
	switch mode {
	case "", "current":
	case "all":
		args = append(args, "--all")
	case "ref":
		if err := validRemoteRef(ref); err != nil {
			return nil, err
		}
		args = append(args, ref, "--")
	default:
		return nil, fmt.Errorf("err.git.unknown_log_mode|%s", mode)
	}
	out, err := g.output(ctx, abs, args...)
	if err != nil {
		return nil, err
	}
	return workspace.ParseLog(out), nil
}

func (g *remoteGit) refs(ctx context.Context, repoRel string) ([]workspace.GitRef, error) {
	if err := g.ensureGit(ctx); err != nil {
		return nil, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return nil, err
	}
	out, err := g.output(ctx, abs, "for-each-ref",
		"--format=%(refname)%00%(refname:short)%00%(HEAD)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var refs []workspace.GitRef
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
		refs = append(refs, workspace.GitRef{
			Name:    short,
			Short:   short,
			Kind:    kind,
			Current: head == "*",
		})
	}
	return refs, nil
}

func (g *remoteGit) fetchAll(ctx context.Context, repoRel string) error {
	if err := g.ensureGit(ctx); err != nil {
		return err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return err
	}
	return g.runAt(ctx, abs, "fetch", "--all")
}

func validRemoteCommitHash(hash string) error {
	hash = strings.TrimSpace(strings.ToLower(hash))
	if len(hash) < 7 || len(hash) > 40 {
		return fmt.Errorf("err.git.empty_branch_name")
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("err.git.empty_branch_name")
		}
	}
	return nil
}

func (g *remoteGit) commitStat(ctx context.Context, repoRel, hash string) (workspace.CommitStat, error) {
	if err := g.ensureGit(ctx); err != nil {
		return workspace.CommitStat{}, err
	}
	if err := validRemoteCommitHash(hash); err != nil {
		return workspace.CommitStat{}, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return workspace.CommitStat{}, err
	}
	out, err := g.output(ctx, abs, "show", "--format=", "--shortstat", hash)
	if err != nil {
		return workspace.CommitStat{}, err
	}
	return workspace.ParseShortstat(string(out)), nil
}

func (g *remoteGit) commitDiff(ctx context.Context, repoRel, hash string) (workspace.DiffResult, error) {
	if err := g.ensureGit(ctx); err != nil {
		return workspace.DiffResult{}, err
	}
	if err := validRemoteCommitHash(hash); err != nil {
		return workspace.DiffResult{}, err
	}
	abs, err := resolveRemoteRepo(g.r.root, repoRel)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	stdout, stderr, err := g.run(ctx, abs, nil, "show", "--format=", "--patch", strings.TrimSpace(hash))
	out := append(stdout, stderr...)
	if err != nil && len(out) == 0 {
		if isGitNotFound(err, stderr) {
			return workspace.DiffResult{}, errRemoteGitUnavailable
		}
		return workspace.DiffResult{}, remoteGitErr(out, err)
	}
	text := string(out)
	binary := strings.Contains(text, "Binary files ") || strings.Contains(text, "GIT binary patch")
	return workspace.DiffResult{Text: text, Binary: binary}, nil
}
