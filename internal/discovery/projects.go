package discovery

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// errEmptyProjectPath 项目文件路径未配置（测试或极简装配场景）。
var errEmptyProjectPath = errors.New("err.projects.empty_file")

// ProjectEntry 是 projects.yaml 中的一条手动项目（local 或 ssh）。
type ProjectEntry struct {
	Kind   string `yaml:"kind,omitempty" json:"kind,omitempty"`
	ConnID string `yaml:"conn_id,omitempty" json:"conn_id,omitempty"`
	Path   string `yaml:"path" json:"path"`
}

// DeletedProject 是一条被逻辑删除（从列表隐藏）的项目记录。
// 保留删除时间，供回收站展示。旧 YAML 可只有 path+at（视为 local）。
type DeletedProject struct {
	Kind   string    `yaml:"kind,omitempty" json:"kind,omitempty"`
	ConnID string    `yaml:"conn_id,omitempty" json:"conn_id,omitempty"`
	Path   string    `yaml:"path" json:"path"`
	At     time.Time `yaml:"at" json:"at"`
}

// projectEntryList 支持 manual 混写：纯字符串 = local；map = ProjectEntry。
type projectEntryList []ProjectEntry

func (l *projectEntryList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.AliasNode {
		value = value.Alias
	}
	if value.Kind != yaml.SequenceNode {
		return fmt.Errorf("manual: want sequence, got kind %v", value.Kind)
	}
	out := make(projectEntryList, 0, len(value.Content))
	for _, n := range value.Content {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		var e ProjectEntry
		switch n.Kind {
		case yaml.ScalarNode:
			e.Kind = KindLocal
			e.Path = n.Value
		case yaml.MappingNode:
			if err := n.Decode(&e); err != nil {
				return err
			}
			if e.Kind == "" {
				e.Kind = KindLocal
			}
		default:
			return fmt.Errorf("manual item: unsupported yaml kind %v", n.Kind)
		}
		out = append(out, e)
	}
	*l = out
	return nil
}

func (l projectEntryList) MarshalYAML() (interface{}, error) {
	out := make([]interface{}, 0, len(l))
	for _, e := range l {
		kind := normalizeEntryKind(e.Kind)
		if kind == KindLocal && e.ConnID == "" {
			out = append(out, e.Path) // 旧形态：纯字符串
			continue
		}
		out = append(out, ProjectEntry{
			Kind:   kind,
			ConnID: e.ConnID,
			Path:   e.Path,
		})
	}
	return out, nil
}

// projectFile 是 projects.yaml 的落盘结构。
type projectFile struct {
	Manual  projectEntryList `yaml:"manual,omitempty"`
	Deleted []DeletedProject `yaml:"deleted,omitempty"`
}

// ProjectStore 是 ~/.kshell/projects.yaml 的内存镜像：
// 手动添加的项目目录（manual）+ 逻辑删除（隐藏）的项目（deleted）。
//
// 本地路径比较走 NormalizePath（Windows 大小写/分隔符不敏感）；
// ssh 按 (kind, connID, NormalizeRemotePath) 比较。
// 落盘保留用户输入的原始写法（local 字符串形态；ssh 为 map）。
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
	for i := range f.Deleted {
		if f.Deleted[i].Kind == "" {
			f.Deleted[i].Kind = KindLocal
		}
	}
	s.mu.Lock()
	s.data = f
	s.mu.Unlock()
	return nil
}

// Manual 返回手动添加的本地项目路径副本（原始写法）。ssh 条目不包含在内，见 ManualEntries。
func (s *ProjectStore) Manual() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data.Manual))
	for _, e := range s.data.Manual {
		if normalizeEntryKind(e.Kind) == KindSSH {
			continue
		}
		out = append(out, e.Path)
	}
	return out
}

// ManualEntries 返回全部手动条目副本（含 local / ssh）。
func (s *ProjectStore) ManualEntries() []ProjectEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ProjectEntry(nil), s.data.Manual...)
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

// IsDeleted 判断引用是否已被隐藏。非 ssh:// 视为本地路径；ssh 按 conn+远端路径归一比较。
func (s *ProjectStore) IsDeleted(refOrPath string) bool {
	e, ok := parseProjectRef(refOrPath)
	if !ok {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isDeletedLocked(e)
}

// Add 把一个本地目录登记为手动项目（等价 AddEntry local）。
func (s *ProjectStore) Add(p string) error {
	return s.AddEntry(ProjectEntry{Kind: KindLocal, Path: p})
}

// AddEntry 登记一条手动项目：去重、顺手从回收站移出（重加即还原）。
func (s *ProjectStore) AddEntry(e ProjectEntry) error {
	e, err := normalizeProjectEntry(e)
	if err != nil {
		return err
	}
	key := projectEntryKey(e)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.data.Manual {
		if projectEntryKey(m) == key {
			return nil // 已在列表里：幂等
		}
	}
	s.data.Manual = append(s.data.Manual, e)
	s.removeDeletedLocked(key)
	return s.saveLocked()
}

// Hide 逻辑删除：记入回收站（带时间）。manual 记录保留，还原时无需重新登记。
// 参数可为本地路径或 ssh:// Ref。
func (s *ProjectStore) Hide(refOrPath string) error {
	e, ok := parseProjectRef(refOrPath)
	if !ok {
		return errors.New("err.projects.empty_path")
	}
	e, err := normalizeProjectEntry(e)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isDeletedLocked(e) {
		return nil // 已隐藏：幂等
	}
	d := DeletedProject{Path: e.Path, At: time.Now()}
	if e.Kind == KindSSH {
		d.Kind = KindSSH
		d.ConnID = e.ConnID
	}
	s.data.Deleted = append(s.data.Deleted, d)
	return s.saveLocked()
}

// Restore 从回收站移出（幂等：不在回收站也不报错）。
func (s *ProjectStore) Restore(refOrPath string) error {
	e, ok := parseProjectRef(refOrPath)
	if !ok {
		return errors.New("err.projects.empty_path")
	}
	e, err := normalizeProjectEntry(e)
	if err != nil {
		return err
	}
	key := projectEntryKey(e)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.removeDeletedLocked(key) {
		return nil
	}
	return s.saveLocked()
}

func (s *ProjectStore) isDeletedLocked(e ProjectEntry) bool {
	key := projectEntryKey(e)
	for _, d := range s.data.Deleted {
		if projectEntryKey(deletedToEntry(d)) == key {
			return true
		}
	}
	return false
}

// removeDeletedLocked 按归一化 key 摘除回收站记录，返回是否有改动。
func (s *ProjectStore) removeDeletedLocked(key string) bool {
	for i, d := range s.data.Deleted {
		if projectEntryKey(deletedToEntry(d)) == key {
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
//   - 剔除目录已不存在的本地项目（会话里的旧 cwd、被删掉的仓库）；
//   - 追加手动登记但扫描没覆盖到的目录（Source = "manual"）；
//   - ssh 手动项始终追加（不走本地 exists），Kind=ssh、Path=远端路径。
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
		kind := w.Kind
		if kind == "" {
			kind = KindLocal
		}
		e := ProjectEntry{Kind: kind, ConnID: w.ConnID, Path: w.Path}
		if st.IsDeleted(workspaceRef(e)) || (kind != KindSSH && !exists(w.Path)) {
			continue
		}
		seen[projectEntryKey(e)] = true
		out = append(out, w)
	}

	for _, e := range st.ManualEntries() {
		e = mustNormalizeEntry(e)
		key := projectEntryKey(e)
		if seen[key] || st.IsDeleted(workspaceRef(e)) {
			continue
		}
		if e.Kind == KindSSH {
			seen[key] = true
			out = append(out, Workspace{
				Path:   e.Path,
				Name:   remoteWorkspaceName(e.Path),
				Source: "manual",
				Kind:   KindSSH,
				ConnID: e.ConnID,
			})
			continue
		}
		if !exists(e.Path) {
			continue
		}
		seen[key] = true
		out = append(out, Workspace{
			Path:   filepath.Clean(e.Path),
			Name:   WorkspaceName(e.Path),
			Source: "manual",
			Kind:   KindLocal,
		})
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

func remoteWorkspaceName(p string) string {
	clean := NormalizeRemotePath(p)
	base := path.Base(clean)
	if base == "/" || base == "." {
		return clean
	}
	return base
}

// DirExists 判断路径存在且是目录。
func DirExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func normalizeEntryKind(kind string) string {
	if kind == "" || kind == KindLocal {
		return KindLocal
	}
	return kind
}

func normalizeProjectEntry(e ProjectEntry) (ProjectEntry, error) {
	e.Kind = normalizeEntryKind(e.Kind)
	e.Path = strings.TrimSpace(e.Path)
	e.ConnID = strings.TrimSpace(e.ConnID)
	if e.Path == "" {
		return ProjectEntry{}, errors.New("err.projects.empty_path")
	}
	switch e.Kind {
	case KindSSH:
		if e.ConnID == "" {
			return ProjectEntry{}, errors.New("err.projects.empty_path")
		}
		e.Path = NormalizeRemotePath(e.Path)
	default:
		e.Kind = KindLocal
		e.ConnID = ""
		e.Path = filepath.Clean(e.Path)
	}
	return e, nil
}

func mustNormalizeEntry(e ProjectEntry) ProjectEntry {
	n, err := normalizeProjectEntry(e)
	if err != nil {
		return e
	}
	return n
}

func projectEntryKey(e ProjectEntry) string {
	kind := normalizeEntryKind(e.Kind)
	if kind == KindSSH {
		return "ssh|" + e.ConnID + "|" + NormalizeRemotePath(e.Path)
	}
	return "local|" + NormalizePath(e.Path)
}

func deletedToEntry(d DeletedProject) ProjectEntry {
	return ProjectEntry{Kind: d.Kind, ConnID: d.ConnID, Path: d.Path}
}

// parseProjectRef 把本地路径或 ssh:// Ref 解析成条目；空串返回 ok=false。
func parseProjectRef(refOrPath string) (ProjectEntry, bool) {
	refOrPath = strings.TrimSpace(refOrPath)
	if refOrPath == "" {
		return ProjectEntry{}, false
	}
	kind, connID, p, err := ParseWorkspaceRef(refOrPath)
	if err != nil {
		return ProjectEntry{}, false
	}
	return ProjectEntry{Kind: kind, ConnID: connID, Path: p}, true
}

func workspaceRef(e ProjectEntry) string {
	if normalizeEntryKind(e.Kind) == KindSSH {
		return FormatSSHRef(e.ConnID, e.Path)
	}
	return e.Path
}
