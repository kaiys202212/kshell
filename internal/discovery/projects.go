package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// errEmptyProjectPath 项目文件路径未配置（测试或极简装配场景）。
var errEmptyProjectPath = errors.New("err.projects.empty_file")

// DeletedProject 是一条被逻辑删除（从列表隐藏）的项目记录。
// 保留删除时间，供回收站展示。
type DeletedProject struct {
	Path string    `yaml:"path" json:"path"`
	At   time.Time `yaml:"at" json:"at"`
}

// projectFile 是 projects.yaml 的落盘结构。
type projectFile struct {
	Manual  []string         `yaml:"manual,omitempty"`
	Deleted []DeletedProject `yaml:"deleted,omitempty"`
}

// ProjectStore 是 ~/.kshell/projects.yaml 的内存镜像：
// 手动添加的项目目录（manual）+ 逻辑删除（隐藏）的项目（deleted）。
//
// 路径比较一律走 NormalizePath（Windows 大小写/分隔符不敏感），
// 但**落盘保留用户输入的原始写法**，免得列表里显示成小写路径。
type ProjectStore struct {
	mu   sync.RWMutex
	path string
	data projectFile
}

func NewProjectStore(path string) *ProjectStore {
	return &ProjectStore{path: path}
}

func (s *ProjectStore) Path() string { return s.path }

// Load 读入项目文件：文件不存在视为空表；内容损坏返回空表 + 错误（调用方可只记日志继续跑）。
func (s *ProjectStore) Load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		s.data = projectFile{}
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}

	var f projectFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		s.mu.Lock()
		s.data = projectFile{}
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.data = f
	s.mu.Unlock()
	return nil
}

// Manual 返回手动添加的项目路径副本（原始写法）。
func (s *ProjectStore) Manual() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.data.Manual...)
}

// Deleted 返回已隐藏项目副本，最近的排前面。
func (s *ProjectStore) Deleted() []DeletedProject {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]DeletedProject(nil), s.data.Deleted...)
	// 落盘顺序即插入顺序，展示时最近的在前更符合回收站直觉。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// IsDeleted 判断路径是否已被隐藏（归一化比较）。
func (s *ProjectStore) IsDeleted(p string) bool {
	key := NormalizePath(p)
	if key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.data.Deleted {
		if NormalizePath(d.Path) == key {
			return true
		}
	}
	return false
}

// Add 把一个目录登记为手动项目：去重、顺手从回收站移出（重加即还原）。
func (s *ProjectStore) Add(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("err.projects.empty_path")
	}
	p = filepath.Clean(p)
	key := NormalizePath(p)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.data.Manual {
		if NormalizePath(m) == key {
			return nil // 已在列表里：幂等
		}
	}
	s.data.Manual = append(s.data.Manual, p)
	s.removeDeletedLocked(key)
	return s.saveLocked()
}

// Hide 逻辑删除：记入回收站（带时间）。manual 记录保留，还原时无需重新登记。
func (s *ProjectStore) Hide(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("err.projects.empty_path")
	}
	p = filepath.Clean(p)
	key := NormalizePath(p)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.data.Deleted {
		if NormalizePath(d.Path) == key {
			return nil // 已隐藏：幂等
		}
	}
	s.data.Deleted = append(s.data.Deleted, DeletedProject{Path: p, At: time.Now()})
	return s.saveLocked()
}

// Restore 从回收站移出（幂等：不在回收站也不报错）。
func (s *ProjectStore) Restore(p string) error {
	key := NormalizePath(strings.TrimSpace(p))
	if key == "" {
		return errors.New("err.projects.empty_path")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.removeDeletedLocked(key) {
		return nil
	}
	return s.saveLocked()
}

// removeDeletedLocked 按归一化 key 摘除回收站记录，返回是否有改动。
func (s *ProjectStore) removeDeletedLocked(key string) bool {
	for i, d := range s.data.Deleted {
		if NormalizePath(d.Path) == key {
			s.data.Deleted = append(s.data.Deleted[:i], s.data.Deleted[i+1:]...)
			return true
		}
	}
	return false
}

// Save 先写临时文件再替换，避免写一半崩溃留下损坏配置（与连接存储同口径）。
func (s *ProjectStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

// saveLocked 在已持有锁时落盘（RWMutex 的写锁是读锁超集，读写锁下都可调用）。
func (s *ProjectStore) saveLocked() error {
	if s.path == "" {
		return errEmptyProjectPath
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ApplyProjects 把项目表叠加到扫描结果上（每次调用都是纯函数，可反复调用）：
//   - 剔除已隐藏的项目；
//   - 剔除目录已不存在的项目（会话里的旧 cwd、被删掉的仓库）；
//   - 追加手动登记但扫描没覆盖到的目录（Source = "manual"）。
//
// st 为 nil 时原样返回（TUI/测试可省）；exists 为 nil 时用 os.Stat 判断目录存在。
//
// 注意：必须从「原始扫描结果」派生，否则 Hide 之后 Restore 回不来。
func ApplyProjects(ws []Workspace, st *ProjectStore, exists func(string) bool) []Workspace {
	if st == nil {
		return ws
	}
	if exists == nil {
		exists = DirExists
	}

	seen := make(map[string]bool, len(ws))
	out := make([]Workspace, 0, len(ws))
	for _, w := range ws {
		if st.IsDeleted(w.Path) || !exists(w.Path) {
			continue
		}
		seen[NormalizePath(w.Path)] = true
		out = append(out, w)
	}

	for _, p := range st.Manual() {
		key := NormalizePath(p)
		if seen[key] || st.IsDeleted(p) || !exists(p) {
			continue
		}
		seen[key] = true
		out = append(out, Workspace{Path: filepath.Clean(p), Name: WorkspaceName(p), Source: "manual"})
	}
	return out
}

// WorkspaceName 取路径的显示名（末级目录名），与扫描聚合的命名口径保持一致。
func WorkspaceName(p string) string {
	clean := filepath.Clean(strings.TrimSpace(p))
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return p
	}
	return filepath.Base(clean)
}

// DirExists 判断路径存在且是目录。
func DirExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
