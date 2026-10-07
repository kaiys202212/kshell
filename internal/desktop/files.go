package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/yangk/kshell/internal/workspace"
)

var (
	errPathOutsideWorkspace = errors.New("err.files.out_of_workspace")
	errDirNotFound          = errors.New("err.files.dir_not_found")
	errInvalidName          = errors.New("err.files.name_invalid")
	errTargetExists         = errors.New("err.files.target_exists")
	errNotDir               = errors.New("err.files.target_not_dir")
	errMoveIntoSelf         = errors.New("err.files.move_into_self")
	errRootUndeletable      = errors.New("err.files.root_not_deletable")
)

// treeCacheKey 区分 showAll=false / true 两棵缓存树，避免过滤语义互相污染。
func treeCacheKey(wsPath string, showAll bool) string {
	if showAll {
		return wsPath + "\x00all"
	}
	return wsPath
}

// treeFor 返回工作区的文件树（懒创建，根层已展开）。
// 树按 treeCacheKey(wsPath, showAll) 缓存：节点 Loaded 状态就是目录级缓存，重复 ListFiles 不再读盘。
// 代数（treeGen）防竞态：文件操作作废缓存后，操作前就开始构建的树
// 不得再写回缓存（否则基于旧目录快照的 Loaded 缓存会一直陈旧）；
// 该情况下返回现建树但不缓存——数据仍正确，只是本次不享受缓存。
func (a *App) treeFor(wsPath string, showAll bool) (*workspace.Tree, error) {
	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	key := treeCacheKey(wsPath, showAll)
	a.treeMu.Lock()
	t, ok := a.trees[key]
	a.treeMu.Unlock()
	if ok {
		return t, nil
	}

	gen := atomic.LoadUint64(&a.treeGen)
	exclude := a.snapshot().Config.Exclude
	tree := workspace.NewTree(wsPath, workspace.NewMatcher(wsPath, exclude, showAll))
	if err := tree.Expand(tree.Root); err != nil {
		return nil, err
	}

	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	if atomic.LoadUint64(&a.treeGen) != gen {
		return tree, nil // 期间发生过 rename：旧快照树只当一次性结果
	}
	if t, ok := a.trees[key]; ok {
		return t, nil // 并发双创建时复用先到者，孤儿树直接丢弃
	}
	if a.trees == nil {
		a.trees = make(map[string]*workspace.Tree)
	}
	a.trees[key] = tree
	return tree, nil
}

// ListFiles 列出工作区内 relPath 目录的子项（懒加载：首次经过才读盘）。
// showAll=true 时展示被 ignore/内置排除的条目（.git 仍永不出现）。
// relPath 为空或 "." 时返回根层；目录项已按「目录优先、名称排序」由 Tree 保证。
func (a *App) ListFiles(wsPath, relPath string, showAll bool) ([]workspace.Node, error) {
	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	tree, err := a.treeFor(wsPath, showAll)
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

// PreviewFile 读取文件预览（头部 512KB、最多 DefaultPreviewMaxLines 行，二进制只给元信息）。
// 绑定调用本身已在独立 goroutine 执行，无需 TUI 那样的异步命令包装；
// 每次现读保证拿到最新 mtime 内容，头部读取开销可忽略。
func (a *App) PreviewFile(wsPath, path string) (workspace.Preview, error) {
	resolved, err := a.resolveWorkspaceFile(wsPath, path)
	if err != nil {
		return workspace.Preview{}, err
	}
	p := workspace.PreviewFile(resolved, 0, workspace.DefaultPreviewMaxLines)
	if p.Err != nil {
		return workspace.Preview{}, p.Err
	}
	return p, nil
}

// resolveRealPath 解析 path 的真实落点。
// 先 EvalSymlinks；Windows 目录 junction 常不被其跟随，再以 Readlink 补检。
func resolveRealPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if samePath(resolved, path) {
		if target, rerr := os.Readlink(path); rerr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			resolved = filepath.Clean(target)
		}
	}
	return resolved, nil
}

// resolveWorkspaceFile 把工作区内相对/绝对路径解析为可安全读取的真实路径：
// 先词法校验（Clean + 前缀包含），再 resolveRealPath 复检——
// Windows 上目录 junction 无需管理员权限即可创建，仅词法校验拦不住它。
func (a *App) resolveWorkspaceFile(wsPath, path string) (string, error) {
	cleaned := filepath.Clean(path)
	if !underPath(wsPath, cleaned) {
		return "", errPathOutsideWorkspace
	}
	resolved, err := resolveRealPath(cleaned)
	if err != nil {
		return "", err
	}
	if !underPath(wsPath, resolved) {
		return "", errPathOutsideWorkspace
	}
	return resolved, nil
}

// SearchFiles 递归搜索工作区内名字包含 query 的文件/目录（大小写不敏感），
// 上限 2000 条；忽略规则与文件树一致（.gitignore + 内置排除；showAll 同 ListFiles）。
func (a *App) SearchFiles(wsPath, query string, showAll bool) ([]workspace.SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return []workspace.SearchHit{}, nil
	}
	cleaned := filepath.Clean(strings.TrimSpace(wsPath))
	if cleaned == "." || cleaned == "" {
		return nil, errWorkspaceNotFound
	}
	m := workspace.NewMatcher(cleaned, a.snapshot().Config.Exclude, showAll)
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
// 成功后作废该工作区的树缓存（懒重建）。
func (a *App) RenameEntry(wsPath, relPath, newName string) (string, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == ".." ||
		strings.ContainsAny(newName, `/\`) {
		return "", errInvalidName
	}

	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	// showAll=true：超集树，showAll 可见的忽略项也可改名
	tree, err := a.treeFor(wsPath, true)
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
	resolved, err := filepath.EvalSymlinks(filepath.Dir(oldAbs))
	if err != nil {
		return "", err // 父目录是失效 junction：拒绝改名
	}
	if !underPath(wsPath, resolved) {
		return "", errPathOutsideWorkspace // 父目录 junction 指向工作区外：拒绝
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

	a.invalidateTree(wsPath)

	return newAbs, nil
}

// invalidateTree 作废工作区的文件树缓存（默认与 showAll 双键）并推进树代数
// （见 treeFor 注释：防旧快照树写回缓存）。所有改动文件树的绑定共用。
func (a *App) invalidateTree(wsPath string) {
	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	a.treeMu.Lock()
	delete(a.trees, treeCacheKey(wsPath, false))
	delete(a.trees, treeCacheKey(wsPath, true))
	a.treeMu.Unlock()
	atomic.AddUint64(&a.treeGen, 1)
}

// RefreshFiles 手动作废工作区树缓存，下次 ListFiles 将重新读盘。
func (a *App) RefreshFiles(wsPath string) error {
	cleaned := filepath.Clean(strings.TrimSpace(wsPath))
	if cleaned == "." || cleaned == "" {
		return errWorkspaceNotFound
	}
	a.invalidateTree(cleaned)
	return nil
}

// CreateEntry 在工作区 dirRel 目录下新建文件/目录（isDir 区分）。
// name 只允许最后一段名字；目标已存在报错；成功后作废树缓存。
func (a *App) CreateEntry(wsPath, dirRel, name string, isDir bool) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return "", errInvalidName
	}

	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	// showAll=true：可在忽略目录下新建（超集树含 showAll 可见项）
	tree, err := a.treeFor(wsPath, true)
	if err != nil {
		return "", err
	}
	node, err := a.nodeAt(tree, wsPath, dirRel)
	if err != nil {
		return "", err
	}
	if !node.IsDir {
		return "", errNotDir
	}

	abs := filepath.Join(node.Path, name)
	if !underPath(wsPath, abs) {
		return "", errPathOutsideWorkspace
	}
	resolved, err := filepath.EvalSymlinks(node.Path)
	if err != nil {
		return "", err // 父目录是失效 junction：拒绝新建
	}
	if !underPath(wsPath, resolved) {
		return "", errPathOutsideWorkspace // 父目录 junction 指向工作区外：拒绝
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", errTargetExists
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if isDir {
		if err := os.Mkdir(abs, 0o755); err != nil {
			return "", err
		}
	} else {
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
		if err != nil {
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
	}

	a.invalidateTree(wsPath)
	return abs, nil
}

// DeleteEntry 永久删除工作区内的文件/目录（os.RemoveAll，不进回收站）。
// 工作区根不可删；成功后作废树缓存。
func (a *App) DeleteEntry(wsPath, relPath string) error {
	clean := filepath.Clean(strings.TrimSpace(relPath))
	if clean == "." || clean == "" || clean == string(filepath.Separator) {
		return errRootUndeletable
	}

	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	// showAll=true：超集树，可删除 showAll 可见的忽略项
	tree, err := a.treeFor(wsPath, true)
	if err != nil {
		return err
	}
	node, err := a.nodeAt(tree, wsPath, relPath)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(node.Path)
	if err != nil {
		return err // 目标是失效 junction：拒绝删除
	}
	if !underPath(wsPath, resolved) {
		return errPathOutsideWorkspace // junction 解析后越出工作区：拒绝
	}
	if err := os.RemoveAll(node.Path); err != nil {
		return err
	}

	a.invalidateTree(wsPath)
	return nil
}

// MoveEntry 把工作区内 srcRel 移入 dstDirRel 目录（保留原名）。
// 拒绝移入自身子孙目录；目标已存在报错；原地移动视为 no-op；
// 成功后作废树缓存。
func (a *App) MoveEntry(wsPath, srcRel, dstDirRel string) (string, error) {
	wsPath = filepath.Clean(strings.TrimSpace(wsPath))
	// showAll=true：超集树，可移动 showAll 可见的忽略项
	tree, err := a.treeFor(wsPath, true)
	if err != nil {
		return "", err
	}
	src, err := a.nodeAt(tree, wsPath, srcRel)
	if err != nil {
		return "", err
	}
	dstDir, err := a.nodeAt(tree, wsPath, dstDirRel)
	if err != nil {
		return "", err
	}
	if !dstDir.IsDir {
		return "", errNotDir
	}

	dstAbs := filepath.Join(dstDir.Path, src.Name)
	if !underPath(wsPath, dstAbs) {
		return "", errPathOutsideWorkspace
	}
	// 目录不能移进自己或自己的子孙（underPath 含相等，正好覆盖两种情况）
	if underPath(src.Path, dstAbs) && !samePath(src.Path, dstAbs) {
		return "", errMoveIntoSelf
	}
	if samePath(src.Path, dstAbs) {
		return src.Path, nil // 原地 drop：no-op
	}
	resolved, err := filepath.EvalSymlinks(dstDir.Path)
	if err != nil {
		return "", err // 目标目录是失效 junction：拒绝
	}
	if !underPath(wsPath, resolved) {
		return "", errPathOutsideWorkspace // 目标目录 junction 指向工作区外：拒绝
	}
	if _, err := os.Lstat(dstAbs); err == nil {
		return "", errTargetExists
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(src.Path, dstAbs); err != nil {
		return "", err
	}

	a.invalidateTree(wsPath)
	return dstAbs, nil
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
	Status      map[string]string `json:"Status"`
	IsRepo      bool              `json:"IsRepo"`
	Branch      string            `json:"Branch"`
	DirBranches map[string]string `json:"DirBranches"`
}

// GitStatus 返回工作区的 git 状态（relPath → 状态码）；
// 非 git 仓库返回 IsRepo=false，前端据此隐藏标记。
func (a *App) GitStatus(wsPath string) (GitStatusResult, error) {
	rep, err := workspace.InspectGit(filepath.Clean(strings.TrimSpace(wsPath)))
	if err != nil {
		return GitStatusResult{}, err
	}
	return GitStatusResult{Status: rep.Status, IsRepo: rep.IsRepo, Branch: rep.Branch, DirBranches: rep.DirBranches}, nil
}

// samePath 判断两路径是否同一文件：Windows 大小写不敏感，统一小写比较
// （与 underPath 同口径）。
func samePath(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
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
