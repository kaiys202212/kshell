package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func seedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "node_modules\n")
	write(t, filepath.Join(root, "main.go"), "package main\n")
	write(t, filepath.Join(root, "sub", "a.txt"), "a")
	write(t, filepath.Join(root, "sub", "deep", "b.txt"), "b")
	write(t, filepath.Join(root, "node_modules", "dep", "index.js"), "x")
	return root
}

func TestExpandLoadsOnlyOneLevel(t *testing.T) {
	root := seedTree(t)
	tree := NewTree(root, NewMatcher(root, nil, false))

	if err := tree.Expand(tree.Root); err != nil {
		t.Fatal(err)
	}
	if len(tree.Root.Children) == 0 {
		t.Fatal("root should have children")
	}

	var sub *Node
	for _, c := range tree.Root.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if sub == nil {
		t.Fatalf("sub missing, children: %v", nodeNames(tree.Root.Children))
	}
	if sub.Loaded {
		t.Fatal("sub must not be loaded before it is expanded")
	}

	if err := tree.Expand(sub); err != nil {
		t.Fatal(err)
	}
	if !sub.Loaded || len(sub.Children) == 0 {
		t.Fatal("expanding sub should load its children")
	}
}

func TestExpandAppliesIgnoreRules(t *testing.T) {
	root := seedTree(t)
	tree := NewTree(root, NewMatcher(root, nil, false))
	if err := tree.Expand(tree.Root); err != nil {
		t.Fatal(err)
	}

	for _, name := range nodeNames(tree.Root.Children) {
		if name == "node_modules" {
			t.Fatalf("ignored dir should not show: %v", nodeNames(tree.Root.Children))
		}
		if name == ".git" {
			t.Fatal(".git must never show")
		}
	}
}

func TestToggleShowAllRevealsIgnored(t *testing.T) {
	root := seedTree(t)

	tree := NewTree(root, NewMatcher(root, nil, true))
	if err := tree.Expand(tree.Root); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range nodeNames(tree.Root.Children) {
		if name == "node_modules" {
			found = true
		}
	}
	if !found {
		t.Fatalf("showAll should reveal node_modules: %v", nodeNames(tree.Root.Children))
	}
}

func TestVisibleRowsFlattenExpandedNodes(t *testing.T) {
	root := seedTree(t)
	tree := NewTree(root, NewMatcher(root, nil, false))
	if err := tree.Expand(tree.Root); err != nil {
		t.Fatal(err)
	}

	rows := tree.Visible()
	if len(rows) < 2 {
		t.Fatalf("rows = %d, want at least root children", len(rows))
	}
	if rows[0].Depth != 0 {
		t.Fatalf("first row depth = %d, want 0", rows[0].Depth)
	}
}

func TestExpandFileIsNoop(t *testing.T) {
	root := seedTree(t)
	tree := NewTree(root, NewMatcher(root, nil, false))
	if err := tree.Expand(tree.Root); err != nil {
		t.Fatal(err)
	}

	var file *Node
	for _, c := range tree.Root.Children {
		if !c.IsDir {
			file = c
		}
	}
	if file == nil {
		t.Fatal("no file node found")
	}
	if err := tree.Expand(file); err != nil {
		t.Fatalf("expanding a file must be a no-op, got %v", err)
	}
	if len(file.Children) != 0 {
		t.Fatal("files have no children")
	}
}

func nodeNames(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

var _ = os.ModeDir
