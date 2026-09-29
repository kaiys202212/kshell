package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/yangk/kshell/internal/workspace"
)

const maxBasket = 20 // 与 TUI 一致：篮子条目上限

var (
	errPathOutsideWorkspace = errors.New("路径越出工作区范围")
	errDirNotFound          = errors.New("目录不存在")
	errInvalidName          = errors.New("名称不能为空且不能包含路径分隔符")
	errTargetExists         = errors.New("目标已存在")
)

// treeFor 返回工作区的文件树（懒创建，根层已展开）。
// 树按工作区路径缓存：节点 Loaded 状态就是目录级缓存，重复 ListFiles 不再读盘。
// 代数（treeGen）防竞态：RenameEntry 作废缓存后，rename 前就开始构建的树
// 不得再写回缓存（否则基于旧目录快照的 Loaded 缓存会一直陈旧）；
// 该情况下返回现建树但不缓存——数据仍正确，只是本次不享受缓存。
func (a *App) treeFor(wsPath string) (*workspace.Tree, error) {
	a.treeMu.Lock()
	t, ok := a.trees[wsPath]
	a.treeMu.Unlock()
	if ok {
		return t, nil
	}

	gen := atomic.LoadUint64(&a.treeGen)
	exclude := a.snapshot().Config.Exclude
	tree := workspace.NewTree(wsPath, workspace.NewMatcher(wsPath, exclude, false))
	if err := tree.Expand(tree.Root); err != nil {
		return nil, err
	}

	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	if atomic.LoadUint64(&a.treeGen) != gen {
		return tree, nil // 期间发生过 rename：旧快照树只当一次性结果
	}
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
func (a *App) PreviewFile(wsPath, path string) (workspace.Preview, error) {
	resolved, err := a.resolveWorkspaceFile(wsPath, path)
	if err != nil {
		return workspace.Preview{}, err
	}
	p := workspace.PreviewFile(resolved, 0, 500)
	if p.Err != nil {
		return workspace.Preview{}, p.Err
	}
	return p, nil
}

// resolveWorkspaceFile 把工作区内相对/绝对路径解析为可安全读取的真实路径：
// 先词法校验（Clean + 前缀包含），再 EvalSymlinks 解析真实落点复检——
// Windows 上目录 junction 无需管理员权限即可创建，仅词法校验拦不住它。
func (a *App) resolveWorkspaceFile(wsPath, path string) (string, error) {
	cleaned := filepath.Clean(path)
	if !underPath(wsPath, cleaned) {
		return "", errPathOutsideWorkspace
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}
	if !underPath(wsPath, resolved) {
		return "", errPathOutsideWorkspace
	}
	return resolved, nil
}

// SearchFiles 递归搜索工作区内名字包含 query 的文件/目录（大小写不敏感），
// 上限 2000 条；忽略规则与文件树一致（.gitignore + 内置排除）。
func (a *App) SearchFiles(wsPath, query string) ([]workspace.SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return []workspace.SearchHit{}, nil
	}
	cleaned := filepath.Clean(strings.TrimSpace(wsPath))
	if cleaned == "." || cleaned == "" {
		return nil, errWorkspaceNotFound
	}
	m := workspace.NewMatcher(cleaned, a.snapshot().Config.Exclude, false)
	hits, err := workspace.SearchFiles(cleaned, query, m, 2000)
	if err != nil {
		return nil, err // root 不存在/读不了要浮出，不能让前端当成「无结果」
	}
	if hits == nil {
		hits = []workspace.SearchHit{} // nil 切片经 JSON 是 null，前端空结果判断会失灵
	}
	return hits, nil
}

// RenameEntry 重命名工作区内的文件/目录（只允许改最后一段名字）：
// 目标已存在报错（Windows 下仅大小写变化视为合法改名）；
// 成功后篮子里的旧路径原地换成新路径，并作废该工作区的树缓存（懒重建）。
func (a *App) RenameEntry(wsPath, relPath, newName string) (string, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == ".." ||
		strings.ContainsAny(newName, `/\`) {
		return "", errInvalidName
	}

	tree, err := a.treeFor(wsPath)
	if err != nil {
		return "", err
	}
	node, err := a.nodeAt(tree, wsPath, relPath)
	if err != nil {
		return "", err
	}

	oldAbs := node.Path
	newAbs := filepath.Join(filepath.Dir(oldAbs), newName)
	if !underPath(wsPath, newAbs) {
		return "", errPathOutsideWorkspace
	}
	if _, err := filepath.EvalSymlinks(filepath.Dir(oldAbs)); err != nil {
		return "", err // 父目录是失效 junction：拒绝改名
	}
	if !samePath(oldAbs, newAbs) { // 同名仅大小写变化时 Lstat 拦不住，放行
		if _, err := os.Lstat(newAbs); err == nil {
			return "", errTargetExists
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return "", err
	}

	a.mu.Lock()
	for i, p := range a.basket {
		// 精确命中或位于改名目录之下（目录改名时子路径跟着换前缀）
		if samePath(p, oldAbs) {
			a.basket[i] = newAbs
		} else if underPath(oldAbs, p) {
			if rel, err := filepath.Rel(oldAbs, p); err == nil {
				a.basket[i] = filepath.Join(newAbs, rel)
			}
		}
	}
	a.mu.Unlock()

	a.treeMu.Lock()
	delete(a.trees, wsPath) // 子树缓存链路整体失效，最省事且正确
	a.treeMu.Unlock()
	// 推进树代数：treeFor 里正在构建的旧快照树不得再入缓存
	atomic.AddUint64(&a.treeGen, 1)

	return newAbs, nil
}

// ReadFileForEdit 整读工作区内文本文件供编辑（上限 1MB、拒二进制）。
func (a *App) ReadFileForEdit(wsPath, path string) (workspace.EditContent, error) {
	resolved, err := a.resolveWorkspaceFile(wsPath, path)
	if err != nil {
		return workspace.EditContent{}, err
	}
	return workspace.ReadForEdit(resolved)
}

// SaveFile 把编辑后的文本写回工作区内文件（原子替换，按 eol 还原行尾）。
func (a *App) SaveFile(wsPath, path, text, eol string) error {
	resolved, err := a.resolveWorkspaceFile(wsPath, path)
	if err != nil {
		return err
	}
	return workspace.SaveEdit(resolved, text, eol)
}

// GitStatusResult 是 GitStatus 的返回形态；Wails 绑定不支持多返回值，
// 故用结构体打包。Status 的键是 git 原生输出的 '/' 分隔相对路径。
type GitStatusResult struct {
	Status map[string]string `json:"Status"`
	IsRepo bool              `json:"IsRepo"`
}

// GitStatus 返回工作区的 git 状态（relPath → 状态码）；
// 非 git 仓库返回 IsRepo=false，前端据此隐藏标记。
func (a *App) GitStatus(wsPath string) (GitStatusResult, error) {
	status, isRepo, err := workspace.GitStatus(filepath.Clean(strings.TrimSpace(wsPath)))
	if err != nil {
		return GitStatusResult{}, err
	}
	return GitStatusResult{Status: status, IsRepo: isRepo}, nil
}

// samePath 判断两路径是否同一文件：Windows 大小写不敏感，统一小写比较
// （与 underPath 同口径）。
func samePath(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
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
