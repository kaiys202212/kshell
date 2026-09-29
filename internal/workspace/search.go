package workspace

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// SearchHit 是一次递归搜索的命中项。
type SearchHit struct {
	Node
	RelPath string // 以 '/' 分隔的相对路径，前端展示「文件名 + 所在目录」用
}

// SearchFiles 从 root 递归搜索名字包含 query 的文件/目录（大小写不敏感子串匹配）。
// 忽略规则由 matcher 决定（.gitignore + 内置排除，.git 永不进结果）；
// 命中达到 max 即停（max<=0 视为 1）；结果按目录遍历顺序返回。
// root 本身读不了时返回 error（子树单个目录读不了只跳过，不算失败）。
func SearchFiles(root, query string, m *Matcher, max int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if max <= 0 {
		max = 1
	}
	lower := strings.ToLower(query)

	var hits []SearchHit
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // 根都进不去，整体失败
			}
			return fs.SkipDir // 单个子树读不了（权限等）不影响其余命中
		}
		if path == root {
			return nil
		}
		if m != nil && m.Skip(path, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(d.Name()), lower) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = d.Name()
		}
		hits = append(hits, SearchHit{
			Node:    Node{Name: d.Name(), Path: path, IsDir: d.IsDir()},
			RelPath: filepath.ToSlash(rel),
		})
		if len(hits) >= max {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return hits, nil
}
