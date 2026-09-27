package workspace

import (
	"os"
	"path/filepath"
	"sort"
)

// Node 是文件树的一个节点；目录只有被展开时才读盘（懒加载）。
type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Expanded bool
	Loaded   bool
	Children []*Node
}

// Row 是渲染用的扁平行（带缩进层级）。
type Row struct {
	Depth int
	Node  *Node
}

type Tree struct {
	Root       *Node
	MaxEntries int
	matcher    *Matcher
}

func NewTree(root string, m *Matcher) *Tree {
	return &Tree{
		Root:       &Node{Name: filepath.Base(root), Path: root, IsDir: true},
		MaxEntries: 1000,
		matcher:    m,
	}
}

// Expand 只加载一层子节点；文件节点是空操作。
func (t *Tree) Expand(n *Node) error {
	if n == nil || !n.IsDir || n.Loaded {
		if n != nil {
			n.Expanded = true
		}
		return nil
	}

	entries, err := os.ReadDir(n.Path)
	if err != nil {
		return err
	}

	count := 0
	for _, e := range entries {
		path := filepath.Join(n.Path, e.Name())
		if t.matcher != nil && t.matcher.Skip(path, e.IsDir()) {
			continue
		}
		count++
		if count > t.MaxEntries {
			break
		}
		n.Children = append(n.Children, &Node{Name: e.Name(), Path: path, IsDir: e.IsDir()})
	}

	sort.SliceStable(n.Children, func(i, j int) bool {
		if n.Children[i].IsDir != n.Children[j].IsDir {
			return n.Children[i].IsDir
		}
		return n.Children[i].Name < n.Children[j].Name
	})

	n.Loaded = true
	n.Expanded = true
	return nil
}

func (t *Tree) Collapse(n *Node) {
	if n == nil {
		return
	}
	n.Expanded = false
}

func (t *Tree) Toggle(n *Node) error {
	if n == nil {
		return nil
	}
	if n.Expanded {
		t.Collapse(n)
		return nil
	}
	return t.Expand(n)
}

// Visible 返回当前展开状态下可渲染的行（根节点本身不渲染）。
func (t *Tree) Visible() []Row {
	var rows []Row
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		for _, c := range n.Children {
			rows = append(rows, Row{Depth: depth, Node: c})
			if c.Expanded {
				walk(c, depth+1)
			}
		}
	}
	walk(t.Root, 0)
	return rows
}
