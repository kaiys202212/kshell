package desktop

import (
	"path/filepath"
	"testing"
)

func TestPathUnderWatchSkip(t *testing.T) {
	root := `D:\ws`
	skip := watchSkipNames
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "node_modules"), true},
		{filepath.Join(root, "node_modules", "x.js"), true},
		{filepath.Join(root, "main.go"), false},
		{filepath.Join(root, ".git"), true},
		{filepath.Join(root, ".git", "HEAD"), true},
		{filepath.Join(root, ".git", "objects", "ab"), true},
	}
	for _, c := range cases {
		if got := pathUnderWatchSkip(c.path, root, skip); got != c.want {
			t.Fatalf("pathUnderWatchSkip(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

func TestIsGitMetaPath(t *testing.T) {
	root := `D:\ws`
	if !isGitMetaPath(filepath.Join(root, ".git", "HEAD")) {
		t.Fatal("HEAD should be meta")
	}
	if !isGitMetaPath(filepath.Join(root, ".git", "index")) {
		t.Fatal("index should be meta")
	}
	if !isGitMetaPath(filepath.Join(root, ".git", "refs", "heads", "master")) {
		t.Fatal("refs/heads/master should be meta")
	}
	if isGitMetaPath(filepath.Join(root, ".git", "objects", "ab", "cd")) {
		t.Fatal("objects should not be meta")
	}
	if isGitMetaPath(filepath.Join(root, "main.go")) {
		t.Fatal("main.go should not be meta")
	}
}
