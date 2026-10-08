package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitDiff_第二提交含新增内容(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "one\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "first")

	writeFile(t, filepath.Join(root, "a.txt"), "one\ntwo\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "second")

	logs, err := Log(root, "", "current", "", 5, 0)
	if err != nil || len(logs) < 2 {
		t.Fatalf("Log: n=%d err=%v", len(logs), err)
	}
	second := logs[0] // 新→旧
	d, err := CommitDiff(root, "", second.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if d.Binary {
		t.Fatal("不应标 binary")
	}
	if !strings.Contains(d.Text, "+two") {
		t.Fatalf("第二提交 diff 应含 +two: %q", d.Text)
	}
}

func TestCommitDiff_拒绝非法hash(t *testing.T) {
	root := t.TempDir()
	if _, err := CommitDiff(root, "", ".."); err == nil {
		t.Fatal("期望拒绝")
	}
	_ = os.TempDir()
}
