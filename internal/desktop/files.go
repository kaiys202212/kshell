package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/workspace"
)

const maxBasket = 20 // 与 TUI 一致：篮子条目上限

var (
	errPathOutsideWorkspace = errors.New("路径越出工作区范围")
	errDirNotFound          = errors.New("目录不存在")
)

// treeFor 返回工作区的文件树（懒创建，根层已展开）。
// 树按工作区路径缓存：节点 Loaded 状态就是目录级缓存，重复 ListFiles 不再读盘。
func (a *App) treeFor(wsPath string) (*workspace.Tree, error) {
	a.treeMu.Lock()
	t, ok := a.trees[wsPath]
	a.treeMu.Unlock()
	if ok {
		return t, nil
	}

	exclude := a.snapshot().Config.Exclude
	tree := workspace.NewTree(wsPath, workspace.NewMatcher(wsPath, exclude, false))
	if err := tree.Expand(tree.Root); err != nil {
		return nil, err
	}

	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	if t, ok := a.trees[wsPath]; ok {
		return t, nil // 并发双创建时复用先到者，孤儿树直接丢弃
	}
	if a.trees == nil {
		a.trees = make(map[string]*workspace.Tree)
	}
	a.trees[wsPath] = tree
	return tree, nil
}

// ListFiles 列出工作区内 relPath 目录的子项（懒加载：首次经过才读盘）。
// relPath 为空或 "." 时返回根层；目录项已按「目录优先、名称排序」由 Tree 保证。
func (a *App) ListFiles(wsPath, relPath string) ([]workspace.Node, error) {
	tree, err := a.treeFor(wsPath)
	if err != nil {
		return nil, err
	}

	node, err := a.nodeAt(tree, wsPath, relPath)
	if err != nil {
		return nil, err
	}

	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	if err := tree.Expand(node); err != nil {
		return nil, err
	}
	out := make([]workspace.Node, 0, len(node.Children))
	for _, c := range node.Children {
		out = append(out, *c)
	}
	return out, nil
}

// nodeAt 从树根按 relPath 逐段下钻定位节点；沿途目录自动展开（保持缓存链完整）。
// 同时做路径校验：解析结果必须仍在工作区内（绑定入口防目录穿越）。
func (a *App) nodeAt(tree *workspace.Tree, wsPath, relPath string) (*workspace.Node, error) {
	clean := filepath.Clean(strings.TrimSpace(relPath))
	if clean == "." || clean == "" {
		return tree.Root, nil
	}
	target := filepath.Join(wsPath, clean)
	if !underPath(wsPath, target) {
		return nil, errPathOutsideWorkspace
	}

	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	node := tree.Root
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if seg == "." {
			continue
		}
		if err := tree.Expand(node); err != nil {
			return nil, err
		}
		var next *workspace.Node
		for _, c := range node.Children {
			if c.Name == seg {
				next = c
				break
			}
		}
		if next == nil {
			return nil, errDirNotFound
		}
		node = next
	}
	return node, nil
}

// PreviewFile 读取文件预览（头部 512KB、最多 500 行，二进制只给元信息）。
// 绑定调用本身已在独立 goroutine 执行，无需 TUI 那样的异步命令包装；
// 每次现读保证拿到最新 mtime 内容，头部读取开销可忽略。
// 防御纵深：路径先做词法校验，再经 EvalSymlinks 解析真实落点复检——
// Windows 上目录 junction 无需管理员权限即可创建，仅词法校验拦不住它。
func (a *App) PreviewFile(wsPath, path string) (workspace.Preview, error) {
	cleaned := filepath.Clean(path)
	if !underPath(wsPath, cleaned) {
		return workspace.Preview{}, errPathOutsideWorkspace
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return workspace.Preview{}, err
	}
	if !underPath(wsPath, resolved) {
		return workspace.Preview{}, errPathOutsideWorkspace
	}
	p := workspace.PreviewFile(resolved, 0, 500)
	if p.Err != nil {
		return workspace.Preview{}, p.Err
	}
	return p, nil
}

// ToggleBasket 把文件加入/移出上下文篮（去重、上限 maxBasket），
// 返回操作后该文件是否在篮中。
func (a *App) ToggleBasket(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, p := range a.basket {
		if p == path {
			a.basket = append(a.basket[:i], a.basket[i+1:]...)
			return false
		}
	}
	if len(a.basket) >= maxBasket {
		return false // 已满：保持原样，该文件不在篮中
	}
	a.basket = append(a.basket, path)
	return true
}

// GetBasket 返回上下文篮内容（副本）。
func (a *App) GetBasket() []string {
	return a.basketSnapshot()
}

// underPath 判断 target 是否在 root 内（含相等）；两端先 Clean 归一。
// Windows 大小写不敏感，统一转小写比较（与 discovery.NormalizePath 同口径）。
func underPath(root, target string) bool {
	root, target = filepath.Clean(root), filepath.Clean(target)
	if os.PathSeparator == '\\' {
		root, target = strings.ToLower(root), strings.ToLower(target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}
