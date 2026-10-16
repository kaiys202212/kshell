package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIgnoreRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "node_modules\n*.log\n")

	m := NewMatcher(root, nil, false)
	if !m.Skip(filepath.Join(root, "node_modules"), true) {
		t.Fatal("node_modules should be skipped")
	}
	if !m.Skip(filepath.Join(root, "debug.log"), false) {
		t.Fatal("*.log should be skipped")
	}
	if m.Skip(filepath.Join(root, "main.go"), false) {
		t.Fatal("main.go must not be skipped")
	}
}

// TestMatcherConcurrentSkip 覆盖隐式并发契约：parserFor 会写 m.parsers 缓存 map，
// 此前靠「调用方持 treeMu」的看不见的约定串行化；共享 Matcher 一旦被并行使用
// （如并行预取）就是并发 map 写崩溃。加锁后 -race 下并发 Skip 必须干净。
func TestMatcherConcurrentSkip(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "*.log\n")
	for i := 0; i < 8; i++ {
		write(t, filepath.Join(root, "d"+string(rune('a'+i)), ".gitignore"), "*.tmp\n")
	}

	m := NewMatcher(root, nil, false)
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				_ = m.Skip(filepath.Join(root, "d"+string(rune('a'+g)), "x.bin"), false)
				_ = m.Skip(filepath.Join(root, "y.log"), false)
			}
		}(g)
	}
	for g := 0; g < 8; g++ {
		<-done
	}
}

func TestBuiltinExcludesApplyWithoutGitignore(t *testing.T) {
	root := t.TempDir()
	m := NewMatcher(root, nil, false)

	if !m.Skip(filepath.Join(root, ".git"), true) {
		t.Fatal(".git must always be excluded")
	}
	if !m.Skip(filepath.Join(root, "node_modules"), true) {
		t.Fatal("node_modules is excluded by default")
	}
	if m.Skip(filepath.Join(root, "src"), true) {
		t.Fatal("normal dirs must not be excluded")
	}
}

func TestShowAllDisablesFiltering(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "node_modules\n")

	m := NewMatcher(root, nil, true)
	if m.Skip(filepath.Join(root, "node_modules"), true) {
		t.Fatal("showAll should reveal ignored entries")
	}
	if !m.Skip(filepath.Join(root, ".git"), true) {
		t.Fatal(".git must stay excluded even with showAll")
	}
}

func TestNestedGitignoreAppliesToItsDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sub", ".gitignore"), "secret.txt\n")

	m := NewMatcher(root, nil, false)
	if !m.Skip(filepath.Join(root, "sub", "secret.txt"), false) {
		t.Fatal("nested .gitignore should apply")
	}
	if m.Skip(filepath.Join(root, "secret.txt"), false) {
		t.Fatal("nested rules must not leak to the parent")
	}
}
