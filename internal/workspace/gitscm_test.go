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
