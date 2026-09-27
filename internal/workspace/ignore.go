package workspace

import (
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"
)

var defaultBuiltin = []string{".git", "node_modules", "vendor", "dist", "build"}

// Matcher 决定文件树里哪些条目不展示：仓库 .gitignore + 内置排除名单。
type Matcher struct {
	root    string
	showAll bool
	builtin map[string]bool
	parsers map[string]ignore.IgnoreParser
}

func NewMatcher(root string, exclude []string, showAll bool) *Matcher {
	names := exclude
	if len(names) == 0 {
		names = defaultBuiltin
	}

	builtin := map[string]bool{".git": true} // .git 永远不进文件树
	for _, n := range names {
		builtin[n] = true
	}

	return &Matcher{
		root:    root,
		showAll: showAll,
		builtin: builtin,
		parsers: map[string]ignore.IgnoreParser{},
	}
}

// SetShowAll 切换「显示全部」：关闭后忽略规则不再生效。
func (m *Matcher) SetShowAll(showAll bool) {
	m.showAll = showAll
}

func (m *Matcher) Skip(path string, isDir bool) bool {
	if m.showAll || path == m.root {
		return false
	}
	if m.builtin[filepath.Base(path)] {
		return true
	}

	// 直接父目录的 .gitignore（就近优先）
	if p := m.parserFor(filepath.Dir(path)); p != nil && p.MatchesPath(filepath.Base(path)) {
		return true
	}
	// 仓库根 .gitignore 对整棵子树生效，用相对路径匹配
	if p := m.parserFor(m.root); p != nil {
		if rel, err := filepath.Rel(m.root, path); err == nil {
			if p.MatchesPath(filepath.ToSlash(rel)) {
				return true
			}
		}
	}
	return false
}

func (m *Matcher) parserFor(dir string) ignore.IgnoreParser {
	if p, ok := m.parsers[dir]; ok {
		return p
	}
	p, err := ignore.CompileIgnoreFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		m.parsers[dir] = nil
		return nil
	}
	m.parsers[dir] = p
	return p
}
