package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SSHOptions 影响 remote 包拼装 ssh 参数的方式，集中配置便于按环境调整。
// BatchMode 不作为配置项：kshell 永远传 -o BatchMode=yes，避免卡在密码/密钥交互提示上。
type SSHOptions struct {
	ConnectTimeout        int      `yaml:"connect_timeout"`
	ExtraArgs             []string `yaml:"extra_args"`
	CommandTimeoutSeconds int      `yaml:"command_timeout_seconds"`
}

// 桌面 UI 字号（px）：body / Agent 聊天 / 内嵌终端共用。
const (
	DefaultUIFontSize = 13
	MinUIFontSize     = 10
	MaxUIFontSize     = 20
)

// ClampUIFontSize 把配置值收进合法区间：未写或 ≤0 用默认 13，其余钳到 10–20。
func ClampUIFontSize(n int) int {
	if n <= 0 {
		return DefaultUIFontSize
	}
	if n < MinUIFontSize {
		return MinUIFontSize
	}
	if n > MaxUIFontSize {
		return MaxUIFontSize
	}
	return n
}

// Appearance 是颜色模式配置：system 跟随操作系统，light/dark 强制覆盖。
type Appearance struct {
	Mode     string `yaml:"mode"`      // system | light | dark
	FontSize int    `yaml:"font_size"` // 10–20，缺省 13
}

// 关闭窗口的行为取值。
const (
	CloseBehaviorTray = "tray" // 收进系统托盘（默认）
	CloseBehaviorExit = "exit" // 直接退出进程
)

// 会话默认打开路径 / 权限模式取值。
const (
	SessionModeTUI = "tui"
	SessionModeACP = "acp"

	PermissionModeDefault = "default"
	PermissionModeBypass  = "bypass"
)

// ModelConfig 是全局模型/端点配置 + 每 agent 模型名。
// BaseURL 为旧字段：仅读取迁移用，保存时 omitempty 且迁移后清空。
type ModelConfig struct {
	Enabled          bool              `yaml:"enabled"`
	Preset           string            `yaml:"preset"`
	OpenAIBaseURL    string            `yaml:"openai_base_url"`
	AnthropicBaseURL string            `yaml:"anthropic_base_url"`
	BaseURL          string            `yaml:"base_url,omitempty"` // legacy
	APIKey           string            `yaml:"api_key"`
	Agents           map[string]string `yaml:"agents"` // toolID -> 模型名（空=不改）
}

type Config struct {
	ScanRoots  []string        `yaml:"scan_roots"`
	MaxDepth   int             `yaml:"max_depth"`
	Exclude    []string        `yaml:"exclude"`
	SSHOptions SSHOptions      `yaml:"ssh"`
	Scanners   map[string]bool `yaml:"scanners"`
	Appearance Appearance      `yaml:"appearance"`
	// Language 界面语言：en / zh-CN 显式指定，system 跟随系统，空串归一为 en。
	Language       string      `yaml:"language"`
	CloseBehavior  string      `yaml:"close_behavior"`  // tray | exit
	SessionMode    string      `yaml:"session_mode"`    // tui | acp
	PermissionMode string      `yaml:"permission_mode"` // default | bypass
	Model          ModelConfig `yaml:"model"`
	// DesktopShortcutEnsured 为 true 后不再自动创建桌面快捷方式（用户删除视为不想要）。
	DesktopShortcutEnsured bool `yaml:"desktop_shortcut_ensured"`
	// AgentSetupDismissed 为 true 后不再自动弹出首次 Agent 安装向导。
	AgentSetupDismissed bool `yaml:"agent_setup_dismissed"`
}

func Default() Config {
	return Config{
		ScanRoots: []string{"~"},
		MaxDepth:  4,
		Exclude:   []string{".git", "node_modules", "vendor", "dist"},
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
		Appearance:     Appearance{Mode: "dark", FontSize: DefaultUIFontSize},
		Language:       "en",
		CloseBehavior:  CloseBehaviorTray,
		SessionMode:    SessionModeTUI,
		PermissionMode: PermissionModeDefault,
		Model:          ModelConfig{Agents: map[string]string{}},
	}
}

// Load 读取配置。缺失用默认值且不报错；语法损坏才备份重建（返回 error 供调用方降级为提示，
// 避免静默清空用户配置）；单个字段类型错误则保留已解析的部分，只把错误交给调用方。
func Load(p Layout) (Config, error) {
	data, err := os.ReadFile(p.Config)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			return c.normalized(), fmt.Errorf("配置文件存在无法解析的字段: %w", err)
		}

		backup := fmt.Sprintf("%s.%d.bak", p.Config, time.Now().Unix())
		if rerr := os.Rename(p.Config, backup); rerr != nil {
			return Default(), fmt.Errorf("备份损坏配置失败: %w", rerr)
		}
		if serr := Save(p, Default()); serr != nil {
			return Default(), fmt.Errorf("重建默认配置失败: %w", serr)
		}
		return Default(), fmt.Errorf("配置文件语法损坏，已备份为 %s 并重建: %w", backup, err)
	}
	return c.normalized(), nil
}

// Save 先写临时文件再替换，避免写一半崩溃留下损坏配置（Windows 的 os.Rename 会覆盖目标文件）。
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
	if c.SSHOptions.ExtraArgs == nil {
		c.SSHOptions.ExtraArgs = []string{}
	}
	// 扫描器开关按 key 合并：用户只写了一项时，其余仍走默认开启，否则等于全关。
	for k, v := range d.Scanners {
		if _, ok := c.Scanners[k]; !ok {
			if c.Scanners == nil {
				c.Scanners = map[string]bool{}
			}
			c.Scanners[k] = v
		}
	}
	// 颜色模式：空值或非法值一律回落默认（dark）。
	switch c.Appearance.Mode {
	case "system", "light", "dark":
	default:
		c.Appearance.Mode = d.Appearance.Mode
	}
	c.Appearance.FontSize = ClampUIFontSize(c.Appearance.FontSize)
	// 界面语言：空值或非法值一律回落默认（en）。
	if c.Language == "" {
		c.Language = d.Language
	}
	switch c.Language {
	case "en", "zh-CN", "system":
	default:
		c.Language = d.Language
	}
	// 关闭行为：空值或非 exit 一律回落默认（tray）。
	if c.CloseBehavior != CloseBehaviorExit {
		c.CloseBehavior = CloseBehaviorTray
	}
	switch c.SessionMode {
	case SessionModeTUI, SessionModeACP:
	default:
		c.SessionMode = d.SessionMode
	}
	switch c.PermissionMode {
	case PermissionModeDefault, PermissionModeBypass:
	default:
		c.PermissionMode = d.PermissionMode
	}
	// 模型配置：Agents 补空 map；旧 base_url 迁移到双协议字段后清空。
	if c.Model.Agents == nil {
		c.Model.Agents = map[string]string{}
	}
	for k, v := range c.Model.Agents {
		c.Model.Agents[k] = strings.TrimSpace(v)
	}
	c.Model.Preset = strings.TrimSpace(c.Model.Preset)
	c.Model.OpenAIBaseURL = sanitizeHTTPURL(c.Model.OpenAIBaseURL)
	c.Model.AnthropicBaseURL = sanitizeHTTPURL(c.Model.AnthropicBaseURL)
	legacy := sanitizeHTTPURL(c.Model.BaseURL)
	if legacy != "" {
		if c.Model.OpenAIBaseURL == "" {
			c.Model.OpenAIBaseURL = legacy
		}
		if c.Model.AnthropicBaseURL == "" {
			c.Model.AnthropicBaseURL = legacy
		}
	}
	c.Model.BaseURL = ""
	return c
}

func sanitizeHTTPURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return ""
	}
	return u
}
