package discovery

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

// Workspace 是一个被 agent 用过（或被 git 扫描发现）的目录。
type Workspace struct {
	Path         string
	Name         string
	LastUsed     time.Time
	SessionCount int
	ToolCounts   map[string]int
	Source       string // sessions | git
	Kind         string `json:"Kind"`     // local | ssh；空视为 local
	ConnID       string `json:"ConnID"`   // 仅 ssh：连接 ID
	ConnName     string `json:"ConnName"` // 展示用连接名，运行时填充
}

// NormalizePath 统一路径写法：Windows 上大小写不敏感、分隔符可能是 '/' 或 '\'，
// 不归一就会把同一个工作区当成两个。
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	cleaned := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	sep := string(filepath.Separator)
	if len(cleaned) > 3 {
		cleaned = strings.TrimSuffix(cleaned, sep)
	}
	return cleaned
}

func GroupSessions(sessions []providers.Session) []Workspace {
	byKey := make(map[string]*Workspace)

	for _, s := range sessions {
		if strings.TrimSpace(s.Workspace) == "" {
			continue // 没有工作区的会话没法归类，直接跳过
		}
		key := NormalizePath(s.Workspace)

		ws, ok := byKey[key]
		if !ok {
			ws = &Workspace{
				Path:       s.Workspace,
				Name:       filepath.Base(filepath.Clean(s.Workspace)),
				LastUsed:   s.UpdatedAt,
				ToolCounts: map[string]int{},
				Source:     "sessions",
			}
			byKey[key] = ws
		}
		ws.SessionCount++
		ws.ToolCounts[s.ToolID]++
		if s.UpdatedAt.After(ws.LastUsed) {
			ws.LastUsed = s.UpdatedAt
		}
	}

	out := make([]Workspace, 0, len(byKey))
	for _, ws := range byKey {
		out = append(out, *ws)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastUsed.Equal(out[j].LastUsed) {
			return out[i].LastUsed.After(out[j].LastUsed)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// ScanGitRepos 在给定根目录里按深度上限找含 .git 的目录，作为「还没开过会话」的工作区补充来源。
// 注意：exclude 里的 ".git" 必须剔除，否则永远不会命中。
func ScanGitRepos(roots []string, maxDepth int, exclude []string) []Workspace {
	skip := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		if name != ".git" {
			skip[name] = true
		}
	}

	seen := map[string]bool{}
	var out []Workspace

	for _, root := range roots {
		root = ExpandHome(root)
		if root == "" {
			continue
		}
		rootInfo, err := os.Stat(root)
		if err != nil || !rootInfo.IsDir() {
			continue
		}

		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == root {
				return nil
			}
			// .git 也可能是文件（git worktree / submodule），两种形态都要认。
			if d.Name() == ".git" {
				repo := filepath.Dir(path)
				key := NormalizePath(repo)
				if !seen[key] {
					seen[key] = true
					out = append(out, Workspace{Path: repo, Name: filepath.Base(repo), Source: "git"})
				}
				return fs.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
			if skip[d.Name()] {
				return fs.SkipDir
			}
			if depth(root, path) > maxDepth {
				return fs.SkipDir
			}
			return nil
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	if rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// ExpandHome 支持配置里写 "~" 或 "~/xxx"。
func ExpandHome(path string) string {
	if path == "" {
		return ""
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
