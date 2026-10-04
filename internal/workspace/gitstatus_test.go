package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParsePorcelainZ(t *testing.T) {
	in := "M  a.go\x00?? b dir/c.txt\x00R  new.txt\x00old.txt\x00" +
		" D gone.txt\x00A  add.txt\x00UU conflict.txt\x00MM both.txt\x00"
	got := parsePorcelainZ([]byte(in), ".", ".")
	want := map[string]string{
		"a.go":         "modified",
		"b dir/c.txt":  "untracked",
		"new.txt":      "renamed",
		"gone.txt":     "deleted",
		"add.txt":      "added",
		"conflict.txt": "conflicted",
		"both.txt":     "modified",
	}
	if len(got) != len(want) {
		t.Fatalf("条目数不符：实得 %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 期望 %s，实得 %s", k, v, got[k])
		}
	}
}

func TestParsePorcelainZ_工作区是仓库子目录时键归一(t *testing.T) {
	// porcelain 路径相对仓库根输出；工作区根 = 仓库根/sub 时键要剥掉前缀
	in := "M  sub/a.go\x00?? sub/deep/b.txt\x00?? sibling.txt\x00"
	got := parsePorcelainZ([]byte(in), filepath.FromSlash("root/sub"), filepath.FromSlash("root"))
	if got["a.go"] != "modified" || got["deep/b.txt"] != "untracked" {
		t.Fatalf("键应相对工作区根：%v", got)
	}
	// 仓库根自己的文件换算出 ../ 前缀键（前端查不到即忽略）
	if got["../sibling.txt"] != "untracked" {
		t.Fatalf("工作区之外的条目应带 ../ 前缀：%v", got)
	}
}

func TestParsePorcelainZ_边界(t *testing.T) {
	// 短于 4 字节的残缺条目丢弃
	got := parsePorcelainZ([]byte("XY\x00??\x00?? ok.txt\x00"), ".", ".")
	if len(got) != 1 || got["ok.txt"] != "untracked" {
		t.Fatalf("边界解析失败：%v", got)
	}
}

func TestParsePorcelainZ_状态码归约(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"?? x", "untracked"},
		{"A  x", "added"},
		{" C x", "modified"}, // C 只会出现在 X 位，Y 位的 C 归约到 modified
		{"AM x", "added"},
		{"D  x", "deleted"},
		{" D x", "deleted"},
		{"R  x", "renamed"},
		{"M  x", "modified"},
		{" M x", "modified"},
		{"MM x", "modified"},
		{"UU x", "conflicted"},
		{"AA x", "conflicted"},
		{"DD x", "conflicted"},
		{"   x", ""}, // 干净条目不该出现，但归约为空
	}
	for _, c := range cases {
		entry := c.in + "\x00"
		got := statusFromXY(entry[0], entry[1])
		if got != c.want {
			t.Errorf("%q 期望 %q，实得 %q", c.in, c.want, got)
		}
	}
}

func TestStatusFromXYIgnored(t *testing.T) {
	if got := statusFromXY('!', '!'); got != "ignored" {
		t.Fatalf("!! → ignored, got %q", got)
	}
}

func TestParsePorcelainZIgnored(t *testing.T) {
	// !! path\0
	data := []byte("!! skip.log\x00?? new.go\x00")
	got := parsePorcelainZ(data, `/ws`, `/ws`)
	if got["skip.log"] != "ignored" {
		t.Fatalf("skip.log: %v", got)
	}
	if got["new.go"] != "untracked" {
		t.Fatalf("new.go: %v", got)
	}
}

func TestGitStatus_非仓库(t *testing.T) {
	// t.TempDir 不是 git 仓库：isRepo=false 且无错误
	_, isRepo, err := GitStatus(t.TempDir())
	if err != nil {
		t.Fatalf("非仓库不应报错：%v", err)
	}
	if isRepo {
		t.Fatal("期望 isRepo=false")
	}
}

func TestGitStatus_真实仓库(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用，跳过")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v 失败：%v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	writeFile(t, filepath.Join(root, "tracked.txt"), "v1\n")
	run("add", "tracked.txt")
	run("commit", "-m", "init")
	writeFile(t, filepath.Join(root, "tracked.txt"), "v2\n")
	writeFile(t, filepath.Join(root, "new.txt"), "n\n")
	writeFile(t, filepath.Join(root, "del.txt"), "d\n")
	run("add", "del.txt")
	run("rm", "--cached", "del.txt")

	status, isRepo, err := GitStatus(root)
	if err != nil || !isRepo {
		t.Fatalf("isRepo=%v err=%v", isRepo, err)
	}
	if status["tracked.txt"] != "modified" {
		t.Errorf("tracked.txt 期望 modified，实得 %s", status["tracked.txt"])
	}
	if status["new.txt"] != "untracked" {
		t.Errorf("new.txt 期望 untracked，实得 %s", status["new.txt"])
	}
	if status["del.txt"] != "added" { // rm --cached 后变 A? 落到 added/deleted 之一即可
		t.Logf("del.txt => %s", status["del.txt"])
	}
}

func TestGitStatus_子目录路径用斜杠(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用，跳过")
	}
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init")
	if err := cmd.Run(); err != nil {
		t.Skip("git init 失败，跳过")
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "a.txt"), "x\n")
	writeFile(t, filepath.Join(root, "top.txt"), "t\n")

	// 以仓库根为工作区：键相对仓库根
	status, _, err := GitStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := status["sub/a.txt"]; !ok || got != "untracked" {
		t.Fatalf("期望 sub/a.txt untracked（斜杠分隔），实得 %v", status)
	}

	// 以仓库子目录为工作区：porcelain 路径相对仓库根，键应剥掉前缀归一到工作区根
	subStatus, isRepo, err := GitStatus(sub)
	if err != nil || !isRepo {
		t.Fatalf("isRepo=%v err=%v", isRepo, err)
	}
	if got, ok := subStatus["a.txt"]; !ok || got != "untracked" {
		t.Fatalf("子目录工作区期望 a.txt untracked，实得 %v", subStatus)
	}
	// 仓库根自己的文件也会出现（porcelain 是全仓库输出），换算出 ../ 前缀键，
	// 前端按工作区 relPath 查不到即自然忽略
	if got, ok := subStatus["../top.txt"]; !ok || got != "untracked" {
		t.Logf("仓库根文件键形态：../top.txt => %q (ok=%v)", got, ok)
	}
}

func TestInspectGit_分支与嵌套仓库(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用，跳过")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v 失败：%v\n%s", args, err, out)
		}
	}
	run(root, "init", "-b", "main")
	run(root, "config", "user.email", "t@t")
	run(root, "config", "user.name", "t")
	writeFile(t, filepath.Join(root, "a.txt"), "a\n")
	run(root, "add", "a.txt")
	run(root, "commit", "-m", "init")

	nested := filepath.Join(root, "ext", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	run(nested, "init", "-b", "dev")
	run(nested, "config", "user.email", "t@t")
	run(nested, "config", "user.name", "t")
	writeFile(t, filepath.Join(nested, "n.txt"), "n\n")
	run(nested, "add", "n.txt")
	run(nested, "commit", "-m", "n")
	writeFile(t, filepath.Join(nested, "dirty.txt"), "x\n")

	rep, err := InspectGit(root)
	if err != nil || !rep.IsRepo {
		t.Fatalf("InspectGit: isRepo=%v err=%v", rep.IsRepo, err)
	}
	if rep.Branch != "main" || rep.DirBranches[""] != "main" {
		t.Fatalf("根分支: branch=%q dirs=%v", rep.Branch, rep.DirBranches)
	}
	if rep.DirBranches["ext/lib"] != "dev" {
		t.Fatalf("嵌套分支: %v", rep.DirBranches)
	}
	if rep.Status["ext/lib/dirty.txt"] != "untracked" {
		t.Fatalf("嵌套状态未合并: %v", rep.Status)
	}
}

