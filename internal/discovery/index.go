package discovery

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
)

const (
	indexVersion     = 5 // 解析逻辑变更时递增，让旧缓存整体失效（v5：codex resume 续写文件按会话 ID 去重）
	defaultMaxFiles  = 20000
	defaultHeadLimit = 256 * 1024 // codex 导入会话首条真实用户消息可能在 100KB+ 之后
	// 单文件 JSON（Gemini）必须整文件成文才解析得出来，头读太小会整条判失败。
	defaultSingleFileHead = 2 * 1024 * 1024
	defaultTimeout        = 30 * time.Second
)

// Entry 是单个会话文件的缓存记录：mtime + size 未变就直接复用解析结果，
// 避免每次启动都重新解析几十 MB 的 JSONL。
type Entry struct {
	MTime   int64             `json:"mtime"`
	Size    int64             `json:"size"`
	Session providers.Session `json:"session"`
}

type Index struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

func newIndex() *Index {
	return &Index{Version: indexVersion, Entries: map[string]Entry{}}
}

func LoadIndex(path string) *Index {
	data, err := os.ReadFile(path)
	if err != nil {
		return newIndex()
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil || idx.Version != indexVersion {
		return newIndex()
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	return &idx
}

func (idx *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (idx *Index) shouldReuse(path string, mtime, size int64) bool {
	entry, ok := idx.Entries[path]
	return ok && entry.MTime == mtime && entry.Size == size
}

type ScanOptions struct {
	MaxFiles  int
	HeadLimit int
	// git 工作区补充扫描的配置（对应 config.yaml）
	Roots    []string
	MaxDepth int
	Exclude  []string
	Timeout  time.Duration
}

type Result struct {
	Sessions   []providers.Session
	Workspaces []Workspace
	Failed     []string // 解析失败的会话文件，供状态栏提示
}

// Scan 遍历各 provider 的会话目录，优先命中缓存，最后聚合成工作区。
func Scan(home string, ps []providers.Provider, cachePath string, opts ScanOptions) (*Result, error) {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxFiles
	}
	if opts.HeadLimit <= 0 {
		opts.HeadLimit = defaultHeadLimit
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = config.Default().MaxDepth
	}
	if len(opts.Exclude) == 0 {
		opts.Exclude = config.Default().Exclude
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}

	deadline := time.Now().Add(opts.Timeout)
	files, err := collectSessionFiles(home, ps, opts.MaxFiles, deadline)
	if err != nil {
		return nil, err
	}

	idx := LoadIndex(cachePath)
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		entries = make(map[string]Entry, len(files))
		failed  []string
	)

	jobs := make(chan string)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if time.Now().After(deadline) {
					continue // 超时后放弃剩余文件，保证启动不被挂住
				}
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				mtime, size := info.ModTime().UnixNano(), info.Size()

				mu.Lock()
				reuse := idx.shouldReuse(path, mtime, size)
				var entry Entry
				if reuse {
					entry = idx.Entries[path]
				}
				mu.Unlock()

				if !reuse {
					headLimit := opts.HeadLimit
					if filepath.Ext(path) == ".json" && headLimit < defaultSingleFileHead {
						headLimit = defaultSingleFileHead
					}
					head, herr := providers.ReadHead(path, headLimit)
					session, serr := parseWith(path, head, ps, home)
					if herr != nil || serr != nil {
						mu.Lock()
						failed = append(failed, path)
						mu.Unlock()
						continue
					}
					entry = Entry{MTime: mtime, Size: size, Session: *session}
				}

				mu.Lock()
				entries[path] = entry
				mu.Unlock()
			}
		}()
	}

	for _, path := range files {
		jobs <- path
	}
	close(jobs)
	wg.Wait()

	idx.Version = indexVersion
	idx.Entries = entries
	if err := idx.Save(cachePath); err != nil {
		// 缓存写不进去不该影响本次扫描结果
		failed = append(failed, cachePath)
	}

	sessions := make([]providers.Session, 0, len(entries))
	for _, entry := range entries {
		sessions = append(sessions, entry.Session)
	}
	sessions = dedupeSessions(sessions)

	return &Result{
		Sessions:   sessions,
		Workspaces: mergeGitWorkspaces(GroupSessions(sessions), opts),
		Failed:     failed,
	}, nil
}

// dedupeSessions 按 (工具, 会话 ID) 去重。codex 每次 resume/compact 都会新开一个 rollout
// 文件（新文件名、内容继承原会话），但 session_meta 里的 session_id 不变——不去重的
// 话同一个逻辑会话在列表里出现 N 次。合并规则：
//   - 代表文件取 UpdatedAt 最新的（resume <id> 时 codex 自己也会挑最新一份续写）；
//   - 标题优先取创建最早的非空标题（原始文件的首条真实用户消息最完整）；
//   - 时间取并集（最早创建 / 最近更新），消息数取最大值。
func dedupeSessions(sessions []providers.Session) []providers.Session {
	type group struct {
		best     int
		earliest int
		maxMsgs  int
	}
	groups := map[string]*group{}
	order := make([]providers.Session, 0, len(sessions))
	ids := make([]string, 0, len(sessions))

	for i, s := range sessions {
		if s.ID == "" {
			order = append(order, s)
			ids = append(ids, "")
			continue
		}
		key := s.ToolID + "\x00" + s.ID
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{best: i, earliest: i, maxMsgs: s.Messages}
			order = append(order, s)
			ids = append(ids, key)
			continue
		}
		if s.UpdatedAt.After(sessions[g.best].UpdatedAt) ||
			(s.UpdatedAt.Equal(sessions[g.best].UpdatedAt) && s.Messages > sessions[g.best].Messages) {
			g.best = i
		}
		if s.CreatedAt.Before(sessions[g.earliest].CreatedAt) ||
			(s.CreatedAt.Equal(sessions[g.earliest].CreatedAt) && s.Title != "" && sessions[g.earliest].Title == "") {
			g.earliest = i
		}
		if s.Messages > g.maxMsgs {
			g.maxMsgs = s.Messages
		}
	}

	merged := make([]providers.Session, 0, len(order))
	done := map[string]bool{}
	for i, s := range order {
		key := ids[i]
		if key == "" {
			merged = append(merged, s)
			continue
		}
		if done[key] {
			continue
		}
		done[key] = true
		g := groups[key]
		if g.best == g.earliest {
			merged = append(merged, s)
			continue
		}
		base := sessions[g.best]
		first := sessions[g.earliest]
		title := first.Title
		if title == "" {
			title = base.Title
		}
		created, updated := base.CreatedAt, base.UpdatedAt
		if first.CreatedAt.Before(created) {
			created = first.CreatedAt
		}
		if first.UpdatedAt.After(updated) {
			updated = first.UpdatedAt
		}
		merged = append(merged, providers.Session{
			ID: base.ID, ToolID: base.ToolID, Workspace: base.Workspace,
			Title: title, CreatedAt: created, UpdatedAt: updated,
			Messages: g.maxMsgs, Path: base.Path,
		})
	}
	return merged
}

// mergeGitWorkspaces 把 git 扫描发现的工作区追加在会话工作区之后（同名目录不重复出现）。
func mergeGitWorkspaces(fromSessions []Workspace, opts ScanOptions) []Workspace {
	if len(opts.Roots) == 0 {
		return fromSessions
	}

	seen := make(map[string]bool, len(fromSessions))
	for _, w := range fromSessions {
		seen[NormalizePath(w.Path)] = true
	}

	out := fromSessions
	for _, w := range ScanGitRepos(opts.Roots, opts.MaxDepth, opts.Exclude) {
		if seen[NormalizePath(w.Path)] {
			continue
		}
		seen[NormalizePath(w.Path)] = true
		out = append(out, w)
	}
	return out
}

// parseWith 按文件所属 provider 解析：用文件所在目录去匹配 provider 的会话根。
func parseWith(path string, head []byte, ps []providers.Provider, home string) (*providers.Session, error) {
	dir := filepath.Dir(path)
	for _, p := range ps {
		for _, root := range p.SessionRoots(home) {
			if root != "" && isUnder(dir, root) {
				return p.ParseSession(path, head)
			}
		}
	}
	// 兜底：找不到归属就挨个试，谁解析成功算谁的
	for _, p := range ps {
		if s, err := p.ParseSession(path, head); err == nil && s != nil {
			return s, nil
		}
	}
	return nil, os.ErrInvalid
}

func isUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || !filepath.IsAbs(rel) && rel != ".." && !hasDotDotPrefix(rel)
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.' &&
		(len(rel) == 2 || rel[2] == filepath.Separator || rel[2] == '/')
}

func collectSessionFiles(home string, ps []providers.Provider, maxFiles int, deadline time.Time) ([]string, error) {
	seen := map[string]bool{}
	var files []string

	for _, p := range ps {
		pattern := p.SessionFilePattern()
		for _, root := range p.SessionRoots(home) {
			if root == "" {
				continue
			}
			if _, err := os.Stat(root); err != nil {
				continue // 工具没装或目录不存在，直接跳过
			}
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if time.Now().After(deadline) {
					return fs.SkipAll // 网络盘/挂起目录不该拖住启动
				}
				if d.IsDir() {
					return nil
				}
				if ok, _ := filepath.Match(pattern, d.Name()); !ok {
					return nil
				}
				// provider 实现了 PathMatcher 时再按相对路径过滤一次（glob 目录深度）
				if pm, ok := p.(providers.PathMatcher); ok {
					rel, rerr := filepath.Rel(root, path)
					if rerr != nil || !pm.MatchSessionRel(filepath.ToSlash(rel)) {
						return nil
					}
				}
				key := NormalizePath(path)
				if seen[key] {
					return nil
				}
				seen[key] = true
				files = append(files, path)
				if len(files) >= maxFiles {
					return fs.SkipAll
				}
				return nil
			})
			if err != nil {
				return files, err
			}
		}
	}
	return files, nil
}
