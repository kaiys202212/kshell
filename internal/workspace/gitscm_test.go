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

	snap, err := SCMStatus(root, "")
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
	snap, err := SCMStatus(t.TempDir(), "")
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

	snap, err := SCMStatus(root, "ext/lib")
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
	snap, _ := SCMStatus(root, "")
	if e := findEntry(snap, "staged.txt"); e == nil || !e.Staged {
		t.Fatalf("Stage 后应 staged: %+v", snap.Entries)
	}
	if err := Unstage(root, "", []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	snap, _ = SCMStatus(root, "")
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
	snap, _ = SCMStatus(root, "")
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
	snap, _ := SCMStatus(root, "")
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
	snap, _ = SCMStatus(root, "")
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
	snap, _ = SCMStatus(root, "")
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
	if err := Pull(root, ""); err == nil {
		t.Fatal("无 remote 的 pull 应失败")
	}
	if err := Push(root, ""); err == nil {
		t.Fatal("无 remote 的 push 应失败")
	}
	_ = Fetch(root, "") // 部分 git 无 remote 时 fetch 仍成功（空操作）
}
