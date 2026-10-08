package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitSCMBinding(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	env := newFilesEnv(t)
	root := env.root
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := env.app.GitSCM(root, "", "")
	if err != nil || !snap.IsRepo {
		t.Fatalf("GitSCM: %+v err=%v", snap, err)
	}
	if err := env.app.GitStage(root, "", []string{"b.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := env.app.GitCommit(root, "", "add b"); err != nil {
		t.Fatal(err)
	}
	snap, err = env.app.GitSCM(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range snap.Entries {
		if e.Path == "b.txt" {
			t.Fatalf("commit 后不应仍有 b.txt: %+v", snap.Entries)
		}
	}

	if _, err := env.app.GitSCM(root, "..", ""); err == nil {
		t.Fatal("越权 repoRel 应失败")
	}

	logs, err := env.app.GitLog(root, "", "current", "", 10, 0)
	if err != nil || len(logs) < 2 {
		t.Fatalf("GitLog: n=%d err=%v", len(logs), err)
	}
	refs, err := env.app.GitRefs(root, "")
	if err != nil || len(refs) < 1 {
		t.Fatalf("GitRefs: %+v err=%v", refs, err)
	}

	diff, err := env.app.GitCommitDiff(root, "", logs[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Text, "+b") {
		t.Fatalf("GitCommitDiff 应含第二提交内容: %q", diff.Text)
	}
}
