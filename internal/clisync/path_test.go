package clisync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EnsurePathUnix：PATH 已含目标目录时不动 rc；缺失时向各 rc 追加标记行且幂等。
func TestEnsurePathUnix(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	rcs := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
	}

	// PATH 未含 → 追加。
	state, err := ensurePathUnix(binDir, "/usr/bin:/bin", rcs)
	if err != nil || state != pathAdded {
		t.Fatalf("state=%q err=%v, want added", state, err)
	}
	for _, rc := range rcs {
		raw, err := os.ReadFile(rc)
		if err != nil {
			t.Fatalf("%s 未创建: %v", rc, err)
		}
		if !strings.Contains(string(raw), binDir) || !strings.Contains(string(raw), rcMarker) {
			t.Fatalf("%s 应包含标记与目录:\n%s", rc, raw)
		}
	}

	// 幂等：重复执行不重复追加。
	if state, _ := ensurePathUnix(binDir, "/usr/bin:/bin", rcs); state != pathAdded {
		t.Fatalf("二次 state=%q, want added", state)
	}
	for _, rc := range rcs {
		raw, _ := os.ReadFile(rc)
		if got := strings.Count(string(raw), rcMarker); got != 1 {
			t.Fatalf("%s 标记行出现 %d 次, want 1", rc, got)
		}
	}

	// PATH 已含 → ready 且不写 rc。
	state, err = ensurePathUnix(binDir, strings.Join([]string{"/usr/bin", binDir, "/bin"}, string(os.PathListSeparator)), rcs)
	if err != nil || state != pathReady {
		t.Fatalf("state=%q err=%v, want ready", state, err)
	}
}

// splitPathEnv 按 OS 分隔符切 PATH 并清洗。
func TestSplitPathEnv(t *testing.T) {
	got := splitPathEnv(strings.Join([]string{`C:\a`, ``, `C:\b`}, string(os.PathListSeparator)))
	if len(got) != 2 {
		t.Fatalf("splitPathEnv = %v, want 2 项", got)
	}
}

// auditEntries：找出排在 binDir 之前且含 kshell 可执行文件的目录。
func TestAuditEntries(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	stale := filepath.Join(home, "stale")
	after := filepath.Join(home, "after")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(after, 0o755); err != nil {
		t.Fatal(err)
	}
	// stale 与 after 都放一个 kshell 可执行文件。
	for _, d := range []string{stale, after} {
		if err := os.WriteFile(filepath.Join(d, BinaryName()), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	entries := []string{stale, binDir, after}
	got := auditEntries(binDir, entries)
	if len(got) != 1 || got[0] != stale {
		t.Fatalf("auditEntries = %v, want [%s]", got, stale)
	}

	// binDir 不在 PATH：全部命中都算旧副本。
	got = auditEntries(binDir, []string{stale, after})
	if len(got) != 2 {
		t.Fatalf("binDir 缺席时 auditEntries = %v, want 2 项", got)
	}

	// 空目录（无 kshell 文件）不算。
	empty := filepath.Join(home, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := auditEntries(binDir, []string{empty, binDir}); len(got) != 0 {
		t.Fatalf("空目录不应命中: %v", got)
	}
}
