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
