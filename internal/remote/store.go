package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	errMissingHost = errors.New("err.ssh.missing_host")
	errKeyMaterial = errors.New("err.ssh.identity_file_path_only")
	// ErrUnknownConnection 表示按 ID 找不到连接（Update/Delete）。
	ErrUnknownConnection = errors.New("err.ssh.not_in_store")
)

const defaultPort = 22

// Connection 是一条远程连接。
// 结构体里**没有**密码字段：私钥只存路径引用，密码只在运行时询问，从结构上杜绝私密信息落盘。
type Connection struct {
	ID           string    `yaml:"id"`
	Name         string    `yaml:"name"`
	Host         string    `yaml:"host"`
	User         string    `yaml:"user,omitempty"`
	Port         int       `yaml:"port,omitempty"`
	IdentityFile string    `yaml:"identity_file,omitempty"`
	Workspace    string    `yaml:"workspace,omitempty"`
	Source       string    `yaml:"source,omitempty"`
	SourceFile   string    `yaml:"source_file,omitempty"`
	Verified     bool      `yaml:"verified,omitempty"`
	LastUsed     time.Time `yaml:"last_used,omitempty"`
}

func (c Connection) Target() string {
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + c.Host
	}
	return target
}

// Store 是连接的持久化存储。绑定层（桌面版）会在多个 goroutine 调用，
// 内部状态由 mu 保护；All/List 返回副本，调用方持有的切片与内部状态解耦。
type Store struct {
	mu    sync.RWMutex
	path  string
	conns []Connection
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		s.conns = nil
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}

	var conns []Connection
	if err := yaml.Unmarshal(data, &conns); err != nil {
		return fmt.Errorf("err.ssh.parse_file_failed|%w", err)
	}
	s.mu.Lock()
	s.conns = conns
	s.mu.Unlock()
	return nil
}

// Save 先写临时文件再替换，避免写一半崩溃留下损坏配置。
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

// saveLocked 在已持有读锁时落盘（写锁是读锁的超集，RWMutex 允许）。
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s.conns)
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

func (s *Store) Add(c Connection) (Connection, error) {
	if strings.TrimSpace(c.Host) == "" {
		return Connection{}, errMissingHost
	}
	if strings.Contains(strings.ToUpper(c.IdentityFile), "PRIVATE KEY") {
		return Connection{}, errKeyMaterial
	}
	if c.Port <= 0 {
		c.Port = defaultPort
	}
	if c.ID == "" {
		c.ID = newID(c)
	}
	if c.Name == "" {
		c.Name = c.Target()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns = append(s.conns, c)
	return c, s.saveLocked()
}

func (s *Store) Update(c Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.conns {
		if s.conns[i].ID == c.ID {
			if strings.Contains(strings.ToUpper(c.IdentityFile), "PRIVATE KEY") {
				return errKeyMaterial
			}
			s.conns[i] = c
			return s.saveLocked()
		}
	}
	return ErrUnknownConnection
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.conns {
		if c.ID == id {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrUnknownConnection
}

// All 返回全部连接的副本。
func (s *Store) All() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Connection(nil), s.conns...)
}

// Has 判断是否已存在同 host+user+port 的连接，防止重复导入产生重复条目。
func (s *Store) Has(host, user string, port int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.conns {
		if strings.EqualFold(c.Host, host) && strings.EqualFold(c.User, user) && c.Port == port {
			return true
		}
	}
	return false
}

// List 返回绑定到该工作区的连接，外加未绑定工作区的「全局」连接（副本）。
func (s *Store) List(workspace string) []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, 0, len(s.conns))
	for _, c := range s.conns {
		if c.Workspace == "" || c.Workspace == workspace {
			out = append(out, c)
		}
	}
	return out
}

func newID(c Connection) string {
	base := strings.ToLower(c.Target())
	base = strings.NewReplacer(" ", "-", "@", "-", ".", "-", ":", "").Replace(base)
	if base == "" {
		base = "conn"
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano()%100000)
}
