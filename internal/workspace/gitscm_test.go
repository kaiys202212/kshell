package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
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
