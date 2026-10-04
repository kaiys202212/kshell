// Package sessionarchive 持久化「已归档」的磁盘会话 ID。
// 归档不改 agent 自己的会话文件，只在 ~/.kshell/archived.json 里记一份名单，便于还原。
package sessionarchive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type file struct {
	IDs []string `json:"ids"`
}

// Store 是归档名单。方法并发安全。
type Store struct {
	mu   sync.Mutex
	path string
	ids  map[string]struct{}
}

// Open 读取已有名单；文件不存在视为空名单。
func Open(path string) (*Store, error) {
	s := &Store{path: path, ids: map[string]struct{}{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for _, id := range f.IDs {
		if id != "" {
			s.ids[id] = struct{}{}
		}
	}
	return s, nil
}

// Add 把会话标为已归档（重复添加无效果）。
func (s *Store) Add(id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; ok {
		return nil
	}
	s.ids[id] = struct{}{}
	return s.saveLocked()
}

// Remove 把会话从归档名单去掉。
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; !ok {
		return nil
	}
	delete(s.ids, id)
	return s.saveLocked()
}

// Has 报告该会话是否已归档。
func (s *Store) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}

// IDs 返回已归档会话 ID，按字典序，便于测试与界面稳定。
func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idsLocked()
}

func (s *Store) idsLocked() []string {
	out := make([]string, 0, len(s.ids))
	for id := range s.ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(file{IDs: s.idsLocked()}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
