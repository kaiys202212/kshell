package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用，跳过")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v 失败：%v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T, root string) {
	t.Helper()
	gitRun(t, root, "init", "-b", "main")
	gitRun(t, root, "config", "user.email", "t@t")
	gitRun(t, root, "config", "user.name", "t")
	gitRun(t, root, "config", "core.autocrlf", "false")
}

func TestGitSCMErrorsWireKeys(t *testing.T) {
	if errRepoOutside.Error() != "err.git.out_of_workspace" {
		t.Fatalf("outside key = %q", errRepoOutside.Error())
	}
	if errEmptyCommit.Error() != "err.git.empty_commit_msg" {
		t.Fatalf("commit key = %q", errEmptyCommit.Error())
	}
	if errEmptyPaths.Error() != "err.git.no_paths" {
		t.Fatalf("paths key = %q", errEmptyPaths.Error())
	}
	if errEmptyRef.Error() != "err.git.empty_branch_name" {
		t.Fatalf("ref key = %q", errEmptyRef.Error())
	}
}

func TestSCMStatus_分组(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "tracked.txt"), "v1\n")
	gitRun(t, root, "add", "tracked.txt")
	gitRun(t, root, "commit", "-m", "init")

	writeFile(t, filepath.Join(root, "tracked.txt"), "v2\n")
	writeFile(t, filepath.Join(root, "staged.txt"), "s\n")
	gitRun(t, root, "add", "staged.txt")
	writeFile(t, filepath.Join(root, "new.txt"), "n\n")

	snap, err := SCMStatus(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.IsRepo || snap.Branch != "main" {
		t.Fatalf("IsRepo=%v branch=%q", snap.IsRepo, snap.Branch)
	}
	if snap.HasUpstream {
		t.Fatal("无 remote 不应有 upstream")
	}

	byPath := map[string]SCMEntry{}
	for _, e := range snap.Entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["staged.txt"]; !ok || !e.Staged || e.Untracked {
		t.Fatalf("staged.txt: %+v", e)
	}
	if e, ok := byPath["tracked.txt"]; !ok || !e.Unstaged || e.Untracked {
		t.Fatalf("tracked.txt: %+v", e)
	}
	if e, ok := byPath["new.txt"]; !ok || !e.Untracked {
		t.Fatalf("new.txt: %+v", e)
	}

	repos := ListGitRepos(root)
	if len(repos) != 1 || repos[0].Rel != "" || repos[0].Branch != "main" {
		t.Fatalf("ListGitRepos: %+v", repos)
	}
}

func TestSCMStatus_非仓库(t *testing.T) {
	skipIfNoGit(t)
	snap, err := SCMStatus(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.IsRepo {
		t.Fatal("期望非仓库")
	}
}

func TestResolveRepo_拒绝逃逸(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveRepo(root, ".."); err == nil {
		t.Fatal("期望拒绝 ..")
	}
	if _, err := ResolveRepo(root, "../elsewhere"); err == nil {
		t.Fatal("期望拒绝 ../elsewhere")
	}
}

func TestSCMStatus_嵌套仓(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")

	nested := filepath.Join(root, "ext", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, nested)
	writeFile(t, filepath.Join(nested, "n.txt"), "n\n")
	gitRun(t, nested, "add", "n.txt")
	gitRun(t, nested, "commit", "-m", "n")
	writeFile(t, filepath.Join(nested, "dirty.txt"), "x\n")

	repos := ListGitRepos(root)
	if len(repos) != 2 {
		t.Fatalf("期望根+嵌套，got %+v", repos)
	}
	foundNested := false
	for _, r := range repos {
		if r.Rel == "ext/lib" {
			foundNested = true
		}
	}
	if !foundNested {
		t.Fatalf("缺嵌套仓: %+v", repos)
	}

	snap, err := SCMStatus(root, "ext/lib", "")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.IsRepo || snap.RepoRel != "ext/lib" {
		t.Fatalf("snap: %+v", snap)
	}
	found := false
	for _, e := range snap.Entries {
		if e.Path == "dirty.txt" && e.Untracked {
			found = true
		}
	}
	if !found {
		t.Fatalf("嵌套变更: %+v", snap.Entries)
	}
}

func TestStageUnstageCommitDiscard(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "tracked.txt"), "v1\n")
	gitRun(t, root, "add", "tracked.txt")
	gitRun(t, root, "commit", "-m", "init")

	writeFile(t, filepath.Join(root, "staged.txt"), "s\n")
	if err := Stage(root, "", []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := SCMStatus(root, "", "")
	if e := findEntry(snap, "staged.txt"); e == nil || !e.Staged {
		t.Fatalf("Stage 后应 staged: %+v", snap.Entries)
	}
	if err := Unstage(root, "", []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	snap, _ = SCMStatus(root, "", "")
	if e := findEntry(snap, "staged.txt"); e == nil || !e.Untracked || e.Staged {
		t.Fatalf("Unstage 后应 untracked: %+v", e)
	}
	if err := Stage(root, "", []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, "", ""); err == nil {
		t.Fatal("空 message 应失败")
	}
	if err := Commit(root, "", "add staged"); err != nil {
		t.Fatal(err)
	}
	snap, _ = SCMStatus(root, "", "")
	if findEntry(snap, "staged.txt") != nil {
		t.Fatalf("commit 后不应再出现 staged.txt: %+v", snap.Entries)
	}

	writeFile(t, filepath.Join(root, "tracked.txt"), "v2\n")
	if err := Discard(root, "", []string{"tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil || string(got) != "v1\n" {
		t.Fatalf("Discard 已跟踪: %q err=%v", got, err)
	}

	writeFile(t, filepath.Join(root, "gone.txt"), "g\n")
	if err := Discard(root, "", []string{"gone.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !os.IsNotExist(err) {
		t.Fatal("Discard 未跟踪应删除文件")
	}
}

func findEntry(snap SCMSnapshot, path string) *SCMEntry {
	for i := range snap.Entries {
		if snap.Entries[i].Path == path {
			return &snap.Entries[i]
		}
	}
	return nil
}

func TestCreateBranchCheckoutStash(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")

	if err := CreateBranch(root, "", "topic"); err != nil {
		t.Fatal(err)
	}
	br, err := Branches(root, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range br {
		if b == "topic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 topic: %v", br)
	}
	if err := Checkout(root, "", "main"); err != nil {
		t.Fatal(err)
	}
	snap, _ := SCMStatus(root, "", "")
	if snap.Branch != "main" {
		t.Fatalf("checkout main, got %q", snap.Branch)
	}
	if err := Checkout(root, "", "topic"); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "a.txt"), "dirty\n")
	if err := StashPush(root, "", "wip"); err != nil {
		t.Fatal(err)
	}
	snap, _ = SCMStatus(root, "", "")
	if findEntry(snap, "a.txt") != nil {
		t.Fatalf("stash 后应干净: %+v", snap.Entries)
	}
	if len(snap.Stashes) != 1 {
		t.Fatalf("stashes: %+v", snap.Stashes)
	}
	if err := StashApply(root, "", 0); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "dirty\n" {
		t.Fatalf("apply 后内容 %q", got)
	}
	if err := StashDrop(root, "", 0); err != nil {
		t.Fatal(err)
	}
	snap, _ = SCMStatus(root, "", "")
	if len(snap.Stashes) != 0 {
		t.Fatalf("drop 后仍有 stash: %+v", snap.Stashes)
	}
}

func TestFetchPullPush_无远程(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")
	if err := Pull(root, "", ""); err == nil {
		t.Fatal("无 remote 的 pull 应失败")
	}
	if err := Push(root, "", ""); err == nil {
		t.Fatal("无 remote 的 push 应失败")
	}
	_ = Fetch(root, "", "") // 部分 git 无 remote 时 fetch 仍成功（空操作）
}

func TestSCMStatus_同步源默认与切换(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	originDir := t.TempDir()
	gitRun(t, originDir, "init", "--bare", "-b", "main")
	mirrorDir := t.TempDir()
	gitRun(t, mirrorDir, "init", "--bare", "-b", "main")
	gitRun(t, root, "remote", "add", "origin", originDir)
	gitRun(t, root, "remote", "add", "mirror", mirrorDir)

	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "c1")
	gitRun(t, root, "push", "origin", "main")
	gitRun(t, root, "push", "mirror", "main")

	writeFile(t, filepath.Join(root, "b.txt"), "b\n")
	gitRun(t, root, "add", "b.txt")
	gitRun(t, root, "commit", "-m", "c2")
	gitRun(t, root, "push", "mirror", "main")
	gitRun(t, root, "branch", "--set-upstream-to=mirror/main", "main")
	gitRun(t, root, "fetch", "--all")

	// 默认：origin 存在就用 origin（即便 upstream 指向 mirror）
	snap, err := SCMStatus(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "origin" {
		t.Fatalf("默认同步源应 origin, got %q", snap.SyncRemote)
	}
	if len(snap.Remotes) != 2 {
		t.Fatalf("Remotes=%v", snap.Remotes)
	}
	if !snap.HasUpstream || snap.Ahead != 1 || snap.Behind != 0 {
		t.Fatalf("origin 差异 has=%v ahead=%d behind=%d", snap.HasUpstream, snap.Ahead, snap.Behind)
	}

	// 显式 mirror → 按 mirror 算（已推送，ahead=0）
	snap, err = SCMStatus(root, "", "mirror")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "mirror" || !snap.HasUpstream || snap.Ahead != 0 || snap.Behind != 0 {
		t.Fatalf("mirror: sync=%q has=%v ahead=%d behind=%d", snap.SyncRemote, snap.HasUpstream, snap.Ahead, snap.Behind)
	}

	// 显式不存在的 remote → 回退默认 origin
	snap, err = SCMStatus(root, "", "nope")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "origin" {
		t.Fatalf("回退同步源应 origin, got %q", snap.SyncRemote)
	}

	// 选中存在但没有同名远端分支的源 → 无同步引用
	emptyDir := t.TempDir()
	gitRun(t, emptyDir, "init", "--bare", "-b", "main")
	gitRun(t, root, "remote", "add", "empty", emptyDir)
	snap, err = SCMStatus(root, "", "empty")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "empty" || snap.HasUpstream {
		t.Fatalf("empty: sync=%q has=%v", snap.SyncRemote, snap.HasUpstream)
	}
}

func TestSCMStatus_无origin默认取upstream源(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	upDir := t.TempDir()
	gitRun(t, upDir, "init", "--bare", "-b", "main")
	gitRun(t, root, "remote", "add", "backup", upDir)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "c1")
	gitRun(t, root, "push", "-u", "backup", "main")

	snap, err := SCMStatus(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "backup" || !snap.HasUpstream {
		t.Fatalf("无 origin 应取 upstream 源 backup: sync=%q has=%v", snap.SyncRemote, snap.HasUpstream)
	}

	// 只有一个 remote 且无 upstream → 取第一个 remote
	plain := t.TempDir()
	initRepo(t, plain)
	writeFile(t, filepath.Join(plain, "a.txt"), "a\n")
	gitRun(t, plain, "add", "a.txt")
	gitRun(t, plain, "commit", "-m", "c1")
	gitRun(t, plain, "remote", "add", "solo", upDir)
	snap, err = SCMStatus(plain, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "solo" || snap.HasUpstream {
		t.Fatalf("单 remote 默认: sync=%q has=%v", snap.SyncRemote, snap.HasUpstream)
	}
}

func TestFetchPullPush_按同步源(t *testing.T) {
	skipIfNoGit(t)
	upstream := t.TempDir()
	initRepo(t, upstream)
	// 夹具允许推入上游已检出分支（真实远端为裸仓，此处仅为省一个裸仓）
	gitRun(t, upstream, "config", "receive.denyCurrentBranch", "ignore")
	writeFile(t, filepath.Join(upstream, "a.txt"), "a\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "c1")

	clone := t.TempDir()
	gitRun(t, clone, "clone", upstream, ".")
	// 系统级 core.autocrlf=true 会让检出变 CRLF；改 false 后重写工作区，按字节断言换行
	gitRun(t, clone, "config", "core.autocrlf", "false")
	gitRun(t, clone, "checkout", "--", ".")

	// 上游前进一步，显式按 origin 拉取
	writeFile(t, filepath.Join(upstream, "a.txt"), "a2\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "c2")
	if err := Pull(clone, "", "origin"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(clone, "a.txt"))
	if err != nil || string(got) != "a2\n" {
		t.Fatalf("pull 后内容 %q err=%v", got, err)
	}

	// 显式按 origin 推送与 fetch
	writeFile(t, filepath.Join(clone, "b.txt"), "b\n")
	gitRun(t, clone, "add", "b.txt")
	gitRun(t, clone, "commit", "-m", "c3")
	if err := Push(clone, "", "origin"); err != nil {
		t.Fatal(err)
	}
	if err := Fetch(clone, "", "origin"); err != nil {
		t.Fatal(err)
	}
	// 不存在的 remote 报错
	if err := Push(clone, "", "nope"); err == nil {
		t.Fatal("不存在的 remote push 应失败")
	}
}

// gitRev 取某仓某表达式的输出（测试用，取 tip/ref 值）。
func gitRev(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// 异名跟踪：本地 fix 跟踪 origin/bugfix，pull/push/差异都必须落到 bugfix。
func TestPullPush_异名远端分支(t *testing.T) {
	skipIfNoGit(t)
	upstream := t.TempDir()
	initRepo(t, upstream)
	// 夹具允许推入上游已检出分支（真实远端为裸仓，此处仅为省一个裸仓）
	gitRun(t, upstream, "config", "receive.denyCurrentBranch", "ignore")

	writeFile(t, filepath.Join(upstream, "a.txt"), "a\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "c1")
	gitRun(t, upstream, "branch", "bugfix")
	gitRun(t, upstream, "switch", "bugfix")
	writeFile(t, filepath.Join(upstream, "b.txt"), "b\n")
	gitRun(t, upstream, "add", "b.txt")
	gitRun(t, upstream, "commit", "-m", "c2")
	gitRun(t, upstream, "switch", "main")

	clone := t.TempDir()
	gitRun(t, clone, "clone", upstream, ".")
	gitRun(t, clone, "config", "core.autocrlf", "false")
	gitRun(t, clone, "checkout", "--", ".")
	gitRun(t, clone, "checkout", "-b", "fix", "origin/bugfix")

	// 上游 bugfix 再前进一格，fetch 后差异应按 origin/bugfix 算（origin/fix 不存在）
	gitRun(t, upstream, "switch", "bugfix")
	writeFile(t, filepath.Join(upstream, "b.txt"), "b2\n")
	gitRun(t, upstream, "add", "b.txt")
	gitRun(t, upstream, "commit", "-m", "c3")
	if err := Fetch(clone, "", "origin"); err != nil {
		t.Fatal(err)
	}
	snap, err := SCMStatus(clone, "", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "origin" || !snap.HasUpstream || snap.Ahead != 0 || snap.Behind != 1 {
		t.Fatalf("异名跟踪差异应按 origin/bugfix: sync=%q has=%v ahead=%d behind=%d",
			snap.SyncRemote, snap.HasUpstream, snap.Ahead, snap.Behind)
	}

	// pull 按 upstream 真名拉 bugfix（同名假设会报 couldn't find remote ref）
	if err := Pull(clone, "", "origin"); err != nil {
		t.Fatalf("pull 异名远端分支应成功: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(clone, "b.txt"))
	if err != nil || string(got) != "b2\n" {
		t.Fatalf("pull 后 b.txt %q err=%v", got, err)
	}

	// push 必须更新远端 bugfix，而不是静默新建 fix 分支
	writeFile(t, filepath.Join(clone, "c.txt"), "c\n")
	gitRun(t, clone, "add", "c.txt")
	gitRun(t, clone, "commit", "-m", "c4")
	if err := Push(clone, "", "origin"); err != nil {
		t.Fatalf("push 异名远端分支应成功: %v", err)
	}
	localTip := gitRev(t, clone, "rev-parse", "HEAD")
	if got := gitRev(t, upstream, "rev-parse", "bugfix"); got != localTip {
		t.Fatalf("远端 bugfix 应等于本地 tip: %s != %s", got, localTip)
	}
	if err := exec.Command("git", "-C", upstream, "show-ref", "--verify", "--quiet", "refs/heads/fix").Run(); err == nil {
		t.Fatal("不应在远端新建 fix 分支")
	}

	// push 后仍按 origin/bugfix 解析：同名假设下 origin/fix 不存在会全 false。
	// push 会同步刷新远端跟踪引用，故此刻 ahead/behind 归零属正常。
	snap, err = SCMStatus(clone, "", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SyncRemote != "origin" || !snap.HasUpstream {
		t.Fatalf("push 后应按 origin/bugfix 算差异: sync=%q has=%v ahead=%d behind=%d",
			snap.SyncRemote, snap.HasUpstream, snap.Ahead, snap.Behind)
	}
}

// detached 与显式无效源：三操作口径一致。
func TestPullPush_detached与无效源(t *testing.T) {
	skipIfNoGit(t)
	upstream := t.TempDir()
	initRepo(t, upstream)
	writeFile(t, filepath.Join(upstream, "a.txt"), "a\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "c1")

	clone := t.TempDir()
	gitRun(t, clone, "clone", upstream, ".")
	gitRun(t, clone, "config", "core.autocrlf", "false")
	gitRun(t, clone, "checkout", "--", ".")
	gitRun(t, clone, "checkout", "--detach", "HEAD")

	if err := Pull(clone, "", "origin"); !errors.Is(err, errNoBranch) {
		t.Fatalf("detached pull 应返回 errNoBranch, got %v", err)
	}
	if err := Push(clone, "", "origin"); !errors.Is(err, errNoBranch) {
		t.Fatalf("detached push 应返回 errNoBranch, got %v", err)
	}
	snap, err := SCMStatus(clone, "", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if snap.HasUpstream {
		t.Fatalf("detached 不应有同步引用: has=%v", snap.HasUpstream)
	}

	// 显式无效源：fetch/pull/push 口径一致
	if err := Fetch(clone, "", "nope"); err == nil {
		t.Fatal("不存在的 remote fetch 应失败")
	}
	if err := Pull(clone, "", "nope"); err == nil {
		t.Fatal("不存在的 remote pull 应失败")
	}
	if err := Push(clone, "", "nope"); err == nil {
		t.Fatal("不存在的 remote push 应失败")
	}
}

// 所选源没有该分支的远端引用时必须报错，而不是让 git 静默新建分支。
func TestPush_源无远端分支时拒绝(t *testing.T) {
	skipIfNoGit(t)
	originDir := t.TempDir()
	gitRun(t, originDir, "init", "--bare", "-b", "main")

	seed := t.TempDir()
	initRepo(t, seed)
	writeFile(t, filepath.Join(seed, "a.txt"), "a\n")
	gitRun(t, seed, "add", "a.txt")
	gitRun(t, seed, "commit", "-m", "c1")
	gitRun(t, seed, "remote", "add", "origin", originDir)
	gitRun(t, seed, "push", "origin", "main")

	clone := t.TempDir()
	gitRun(t, clone, "clone", originDir, ".")
	gitRun(t, clone, "config", "core.autocrlf", "false")
	gitRun(t, clone, "checkout", "--", ".")

	// 本地新分支，远端 origin 上没有同名 feature
	gitRun(t, clone, "checkout", "-b", "feature", "main")
	err := Push(clone, "", "origin")
	if err == nil {
		t.Fatal("远端无同名分支时 push 应报错")
	}
	if !strings.Contains(err.Error(), "feature") {
		t.Fatalf("错误信息应点名分支: %v", err)
	}
	if err := exec.Command("git", "-C", originDir, "show-ref", "--verify", "--quiet", "refs/heads/feature").Run(); err == nil {
		t.Fatal("远端不应被新建 feature 分支")
	}
}

// 用户原始 bug 场景：upstream 手工指向 gitcode，默认 pull/push 仍应落 origin。
func TestPullPush_默认解析操作侧端到端(t *testing.T) {
	skipIfNoGit(t)
	originDir := t.TempDir()
	gitRun(t, originDir, "init", "--bare", "-b", "main")
	gitcodeDir := t.TempDir()
	gitRun(t, gitcodeDir, "init", "--bare", "-b", "main")

	seed := t.TempDir()
	initRepo(t, seed)
	writeFile(t, filepath.Join(seed, "a.txt"), "a\n")
	gitRun(t, seed, "add", "a.txt")
	gitRun(t, seed, "commit", "-m", "c1")
	gitRun(t, seed, "remote", "add", "origin", originDir)
	gitRun(t, seed, "remote", "add", "gitcode", gitcodeDir)
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, seed, "push", "gitcode", "main")
	c1 := gitRev(t, gitcodeDir, "rev-parse", "main")

	clone := t.TempDir()
	gitRun(t, clone, "clone", originDir, ".")
	gitRun(t, clone, "config", "core.autocrlf", "false")
	gitRun(t, clone, "checkout", "--", ".")
	gitRun(t, clone, "remote", "add", "gitcode", gitcodeDir)
	// 夹具内手工把 upstream 指到 gitcode（造数据，非产品代码改 config）
	gitRun(t, clone, "config", "branch.main.remote", "gitcode")

	// origin 前进一步，gitcode 停在 c1
	writeFile(t, filepath.Join(seed, "a.txt"), "a2\n")
	gitRun(t, seed, "add", "a.txt")
	gitRun(t, seed, "commit", "-m", "c2")
	gitRun(t, seed, "push", "origin", "main")
	originC2 := gitRev(t, originDir, "rev-parse", "main")

	// 默认 pull 落 origin（upstream 指向 gitcode 也不能拉错源）
	if err := Pull(clone, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := gitRev(t, clone, "rev-parse", "HEAD"); got != originC2 {
		t.Fatalf("默认 pull 应拉 origin: HEAD=%s origin=%s", got, originC2)
	}
	got, err := os.ReadFile(filepath.Join(clone, "a.txt"))
	if err != nil || string(got) != "a2\n" {
		t.Fatalf("pull 后 a.txt %q err=%v", got, err)
	}

	// 默认 push 落 origin，gitcode 原地不动
	writeFile(t, filepath.Join(clone, "b.txt"), "b\n")
	gitRun(t, clone, "add", "b.txt")
	gitRun(t, clone, "commit", "-m", "c3")
	if err := Push(clone, "", ""); err != nil {
		t.Fatal(err)
	}
	cloneTip := gitRev(t, clone, "rev-parse", "HEAD")
	if got := gitRev(t, originDir, "rev-parse", "main"); got != cloneTip {
		t.Fatalf("默认 push 应更新 origin/main: origin=%s clone=%s", got, cloneTip)
	}
	if got := gitRev(t, gitcodeDir, "rev-parse", "main"); got != c1 {
		t.Fatalf("gitcode/main 不应被改动: %s != %s", got, c1)
	}
}
