package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// 未知 log 模式以 wire key + 参数返回，供前端翻译。
func TestLogUnknownModeWireKey(t *testing.T) {
	_, err := Log(t.TempDir(), "", "bogus", "", 1)
	if err == nil {
		t.Fatal("未知 log 模式应失败")
	}
	if err.Error() != "err.git.unknown_log_mode|bogus" {
		t.Fatalf("err = %q", err.Error())
	}
}

func TestParseShortstat(t *testing.T) {
	st := parseShortstat(" 19 files changed, 1867 insertions(+), 202 deletions(-)\n")
	if st.Files != 19 || st.Insertions != 1867 || st.Deletions != 202 {
		t.Fatalf("%+v", st)
	}
	st = parseShortstat(" 1 file changed, 3 insertions(+)\n")
	if st.Files != 1 || st.Insertions != 3 || st.Deletions != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestCommitStatAt_真实提交(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")
	logs, err := Log(root, "", "current", "", 1)
	if err != nil || len(logs) != 1 {
		t.Fatalf("log: %v %+v", err, logs)
	}
	st, err := CommitStatAt(root, "", logs[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files < 1 {
		t.Fatalf("stat %+v", st)
	}
	if _, err := CommitStatAt(root, "", ".."); err == nil {
		t.Fatal("非法 hash 应失败")
	}
}

func TestLog_越权(t *testing.T) {
	skipIfNoGit(t)
	_, err := Log(t.TempDir(), "..", "all", "", 10)
	if err == nil {
		t.Fatal("越权 repoRel 应失败")
	}
}

func TestLog_合并提交含双亲(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")
	gitRun(t, root, "switch", "-c", "topic")
	writeFile(t, filepath.Join(root, "b.txt"), "b\n")
	gitRun(t, root, "add", "b.txt")
	gitRun(t, root, "commit", "-m", "topic")
	gitRun(t, root, "switch", "main")
	writeFile(t, filepath.Join(root, "c.txt"), "c\n")
	gitRun(t, root, "add", "c.txt")
	gitRun(t, root, "commit", "-m", "mainline")
	gitRun(t, root, "merge", "topic", "-m", "merge topic")

	all, err := Log(root, "", "all", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 {
		t.Fatalf("all log 条数 %d", len(all))
	}
	var merge *LogCommit
	for i := range all {
		if all[i].Subject == "merge topic" {
			merge = &all[i]
			break
		}
	}
	if merge == nil || len(merge.Parents) != 2 {
		t.Fatalf("合并提交: %+v", merge)
	}

	cur, err := Log(root, "", "current", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(cur) != len(all) {
		t.Fatalf("当前在 merge 后应看到全部可达提交: current=%d all=%d", len(cur), len(all))
	}

	topicOnly, err := Log(root, "", "ref", "topic", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range topicOnly {
		if c.Subject == "mainline" {
			t.Fatalf("topic 历史不应含 mainline: %+v", topicOnly)
		}
	}
}

func TestRefs_本地与远端(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	initRepo(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-m", "init")

	bare := t.TempDir()
	gitRun(t, bare, "init", "--bare", "-b", "main")
	gitRun(t, root, "remote", "add", "origin", bare)
	gitRun(t, root, "push", "-u", "origin", "main")

	refs, err := Refs(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var local, remote, current int
	for _, r := range refs {
		if r.Kind == "local" {
			local++
			if r.Name == "main" && r.Current {
				current++
			}
		}
		if r.Kind == "remote" && r.Name == "origin/main" {
			remote++
		}
	}
	if local < 1 || remote < 1 || current != 1 {
		t.Fatalf("refs=%+v local=%d remote=%d current=%d", refs, local, remote, current)
	}
}

func TestFetchAll_从第二远程更新(t *testing.T) {
	skipIfNoGit(t)
	upstream := t.TempDir()
	initRepo(t, upstream)
	writeFile(t, filepath.Join(upstream, "a.txt"), "a\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "init")

	clone := t.TempDir()
	gitRun(t, clone, "clone", upstream, ".")
	if err := FetchAll(clone, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCheckout_跟踪远端分支(t *testing.T) {
	skipIfNoGit(t)
	upstream := t.TempDir()
	initRepo(t, upstream)
	writeFile(t, filepath.Join(upstream, "a.txt"), "a\n")
	gitRun(t, upstream, "add", "a.txt")
	gitRun(t, upstream, "commit", "-m", "init")
	gitRun(t, upstream, "switch", "-c", "feat")
	writeFile(t, filepath.Join(upstream, "f.txt"), "f\n")
	gitRun(t, upstream, "add", "f.txt")
	gitRun(t, upstream, "commit", "-m", "feat")
	gitRun(t, upstream, "switch", "main")

	clone := t.TempDir()
	gitRun(t, clone, "clone", upstream, ".")
	if err := Checkout(clone, "", "origin/feat"); err != nil {
		t.Fatal(err)
	}
	br, err := os.ReadFile(filepath.Join(clone, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(br); got != "ref: refs/heads/feat\n" && got != "ref: refs/heads/feat\r\n" {
		snap, _ := SCMStatus(clone, "")
		if snap.Branch != "feat" {
			t.Fatalf("应检出本地 feat，HEAD=%q branch=%q", got, snap.Branch)
		}
	}
}
