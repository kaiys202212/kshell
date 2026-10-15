package remote

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/atomfile"
	"gopkg.in/yaml.v3"
)

var (
	errMissingHost = errors.New("err.ssh.missing_host")
	errKeyMaterial = errors.New("err.ssh.identity_file_path_only")
	// ErrUnknownConnection 表示按 ID 找不到连接（Update/Delete）。
	// 桌面端由 desktop 层映射为 err.ssh.connection_not_found，store 层哨兵
	// 主要用于 TUI/自包含调用场景。
	ErrUnknownConnection = errors.New("err.ssh.not_in_store")
)

const defaultPort = 22

// Connection 是一条远程连接。
// 私钥只存路径引用（IdentityFile 禁止密钥材料）；密码可明文落盘（Password），
// 供有密码时经 ASKPASS 非交互认证（见后续 BuildArgs/AskPass 路径）。
type Connection struct {
	ID           string    `yaml:"id"`
	Name         string    `yaml:"name"`
	Host         string    `yaml:"host"`
	User         string    `yaml:"user,omitempty"`
	Port         int       `yaml:"port,omitempty"`
	IdentityFile string    `yaml:"identity_file,omitempty"`
	Password     string    `yaml:"password,omitempty"`
	Workspace    string    `yaml:"workspace,omitempty"`
	AllowAgent   bool      `yaml:"allow_agent,omitempty"`
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
	data, err := yaml.Marshal(s.conns)
	if err != nil {
		return err
	}
	return atomfile.Write(s.path, data, 0o755)
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

	s.mu.Lock()
	defer s.mu.Unlock()
	// 生成 ID 时对现有连接查重：UnixNano 后缀熵有限，同 target 快速连续 Add
	//（批量导入候选）会碰撞，冲突的 ID 会让 Update/Delete 打到错误条目。
	if c.ID == "" {
		for {
			c.ID = newID(c)
			if !s.hasIDLocked(c.ID) {
				break
			}
		}
	}
	if c.Name == "" {
		c.Name = c.Target()
	}
	// 先按「加入后」的快照落盘，成功后才改内存：落盘失败时内存不漂移，
	// 否则下次 Load 用磁盘旧数据覆盖，用户刚确认的连接会「消失」。
	saved := append(append([]Connection(nil), s.conns...), c)
	backup := s.conns
	s.conns = saved
	if err := s.saveLocked(); err != nil {
		s.conns = backup
		return Connection{}, err
	}
	return c, nil
}

// hasIDLocked 报告 ID 是否已被占用（调用方持锁）。
func (s *Store) hasIDLocked(id string) bool {
	for _, c := range s.conns {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (s *Store) Update(c Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.conns {
		if s.conns[i].ID == c.ID {
			if strings.Contains(strings.ToUpper(c.IdentityFile), "PRIVATE KEY") {
				return errKeyMaterial
			}
			old := s.conns[i]
			s.conns[i] = c
			if err := s.saveLocked(); err != nil {
				s.conns[i] = old
				return err
			}
			return nil
		}
	}
	return ErrUnknownConnection
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.conns {
		if c.ID == id {
			backup := s.conns
			s.conns = append(append([]Connection(nil), s.conns[:i]...), s.conns[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.conns = backup
				return err
			}
			return nil
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
