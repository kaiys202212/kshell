package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	errMissingHost       = errors.New("连接缺少 host")
	errKeyMaterial       = errors.New("identity_file 只能是密钥路径，不能放密钥内容")
	errUnknownConnection = errors.New("未找到该连接")
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

type Store struct {
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
		s.conns = nil
		return nil
	}
	if err != nil {
		return err
	}

	var conns []Connection
	if err := yaml.Unmarshal(data, &conns); err != nil {
		return fmt.Errorf("解析连接文件失败: %w", err)
	}
	s.conns = conns
	return nil
}

// Save 先写临时文件再替换，避免写一半崩溃留下损坏配置。
func (s *Store) Save() error {
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

	s.conns = append(s.conns, c)
	return c, s.Save()
}

func (s *Store) Update(c Connection) error {
	for i := range s.conns {
		if s.conns[i].ID == c.ID {
			if strings.Contains(strings.ToUpper(c.IdentityFile), "PRIVATE KEY") {
				return errKeyMaterial
			}
			s.conns[i] = c
			return s.Save()
		}
	}
	return errUnknownConnection
}

func (s *Store) Delete(id string) error {
	for i, c := range s.conns {
		if c.ID == id {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return s.Save()
		}
	}
	return errUnknownConnection
}

func (s *Store) All() []Connection {
	return s.conns
}

// Has 判断是否已存在同 host+user+port 的连接，防止重复导入产生重复条目。
func (s *Store) Has(host, user string, port int) bool {
	for _, c := range s.conns {
		if strings.EqualFold(c.Host, host) && strings.EqualFold(c.User, user) && c.Port == port {
			return true
		}
	}
	return false
}

// List 返回绑定到该工作区的连接，外加未绑定工作区的「全局」连接。
func (s *Store) List(workspace string) []Connection {
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
