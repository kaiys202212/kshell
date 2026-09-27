package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// SSHOptions 影响 remote 包拼装 ssh 参数的方式，集中配置便于按环境调整。
// BatchMode 不作为配置项：kshell 永远传 -o BatchMode=yes，避免卡在密码/密钥交互提示上。
type SSHOptions struct {
	ConnectTimeout        int      `yaml:"connect_timeout"`
	ExtraArgs             []string `yaml:"extra_args"`
	CommandTimeoutSeconds int      `yaml:"command_timeout_seconds"`
}

type Config struct {
	ScanRoots  []string        `yaml:"scan_roots"`
	MaxDepth   int             `yaml:"max_depth"`
	Exclude    []string        `yaml:"exclude"`
	SSHOptions SSHOptions      `yaml:"ssh"`
	Scanners   map[string]bool `yaml:"scanners"`
}

func Default() Config {
	return Config{
		ScanRoots: []string{"~"},
		MaxDepth:  4,
		Exclude: []string{".git", "node_modules", "vendor", "dist"},
		SSHOptions: SSHOptions{
			ConnectTimeout:        5,
			ExtraArgs:             []string{},
			CommandTimeoutSeconds: 60,
		},
		Scanners: map[string]bool{
			"sshconfig": true,
			"env":       true,
			"spring":    true,
			"deploy":    true,
			"docs":      true,
		},
	}
}

// Load 读取配置：缺失用默认值；损坏则备份为 .bak 后回退默认值，绝不因配置问题阻断启动。
func Load(p Layout) (Config, error) {
	data, err := os.ReadFile(p.Config)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		_ = os.Rename(p.Config, p.Config+".bak")
		_ = Save(p, Default()) // 重建一份默认配置，避免用户面对空目录
		return Default(), nil
	}
	return c.normalized(), nil
}

// Save 先写临时文件再 rename，避免写一半崩溃留下损坏配置。
func Save(p Layout, c Config) error {
	if err := EnsureRoot(p); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	tmp := p.Config + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p.Config); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (c Config) normalized() Config {
	d := Default()
	if len(c.ScanRoots) == 0 {
		c.ScanRoots = d.ScanRoots
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = d.MaxDepth
	}
	if len(c.Exclude) == 0 {
		c.Exclude = d.Exclude
	}
	if c.SSHOptions.ConnectTimeout <= 0 {
		c.SSHOptions.ConnectTimeout = d.SSHOptions.ConnectTimeout
	}
	if c.SSHOptions.CommandTimeoutSeconds <= 0 {
		c.SSHOptions.CommandTimeoutSeconds = d.SSHOptions.CommandTimeoutSeconds
	}
	if len(c.Scanners) == 0 {
		c.Scanners = d.Scanners
	}
	return c
}
