package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitDiffErrorsWireKeys(t *testing.T) {
	if errBadPatch.Error() != "err.git.hunk_missing_header" {
		t.Fatalf("patch key = %q", errBadPatch.Error())
	}
	if errBadDiffOp.Error() != "err.git.invalid_hunk_op" {
		t.Fatalf("op key = %q", errBadDiffOp.Error())
	}
	if errBadSide.Error() != "err.git.invalid_diff_side" {
		t.Fatalf("side key = %q", errBadSide.Error())
	}
}

func TestFileDiff_workingAndStaged(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "tracked.txt"), "v1\n")
	gitRun(t, root, "add", "tracked.txt")
	gitRun(t, root, "commit", "-m", "init")
	writeFile(t, filepath.Join(root, "tracked.txt"), "v2\n")

	d, err := FileDiff(root, "", "tracked.txt", "working")
	if err != nil {
		t.Fatal(err)
	}
	if d.Binary || !strings.Contains(d.Text, "+v2") || !strings.Contains(d.Text, "-v1") {
		t.Fatalf("working diff: %+v", d)
	}

	if err := Stage(root, "", []string{"tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	st, err := FileDiff(root, "", "tracked.txt", "staged")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.Text, "+v2") {
		t.Fatalf("staged diff: %+v", st)
	}
	wk, err := FileDiff(root, "", "tracked.txt", "working")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(wk.Text) != "" && strings.Contains(wk.Text, "+v2") {
		// 全部已暂存时 working 应为空
		t.Fatalf("working 应为空: %+v", wk)
	}
}

func TestFileDiff_untracked(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")
	writeFile(t, filepath.Join(root, "new.txt"), "n\n")

	d, err := FileDiff(root, "", "new.txt", "working")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Untracked || !strings.Contains(d.Text, "+n") {
		t.Fatalf("untracked diff: %+v", d)
	}
}

func TestApplyHunk_stage(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "tracked.txt"), "v1\n")
	gitRun(t, root, "add", "tracked.txt")
	gitRun(t, root, "commit", "-m", "init")
	writeFile(t, filepath.Join(root, "tracked.txt"), "v2\n")

	d, err := FileDiff(root, "", "tracked.txt", "working")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyHunk(root, "", "tracked.txt", "working", d.Text, "stage"); err != nil {
		t.Fatal(err)
	}
	snap, err := SCMStatus(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(snap, "tracked.txt")
	if e == nil || !e.Staged {
		t.Fatalf("hunk stage 后应 staged: %+v", snap.Entries)
	}
}

func TestApplyHunk_拒绝无头(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")
	if err := ApplyHunk(root, "", "a.txt", "working", "@@ -1 +1 @@\n-a\n+b\n", "stage"); err == nil {
		t.Fatal("无 diff --git 头应拒绝")
	}
}

func TestFileDiff_越权(t *testing.T) {
	root := t.TempDir()
	if _, err := FileDiff(root, "", "../x", "working"); err == nil {
		t.Fatal("期望拒绝")
	}
	_ = os.TempDir()
}