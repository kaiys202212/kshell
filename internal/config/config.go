package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/atomfile"
	"gopkg.in/yaml.v3"
)

// languagePattern 是允许的 locale 码模式：2-3 字母主语言段 + 可选 2-4 字母区域段。
// 外部语言包可引入任意语言（如 ja、pt-BR），故值域放宽到码模式而非白名单；
// 原样保留不做大小写折叠，与既有精确匹配惯例一致。
var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z]{2,4})?$`)

// ValidLanguage 报告语言值是否可持久化：en/zh-CN/system 三个内建值，
// 或匹配 languagePattern 的任意 locale 码；其余（空串、含非法字符）为非法。
// desktop 侧 normalizeLanguage 复用此判定，避免两侧值域漂移。
func ValidLanguage(s string) bool {
	switch s {
	case "en", "zh-CN", "system":
		return true
	}
	return languagePattern.MatchString(s)
}

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
	Mode           string `yaml:"mode"`            // system | light | dark
	FontSize       int    `yaml:"font_size"`       // 10–20，缺省 13
	ShowWhitespace bool   `yaml:"show_whitespace"` // 编辑器显示空白字符，默认 false
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

// ModelPlatform 是一个模型平台（供应商）配置：独立端点与密钥。
type ModelPlatform struct {
	ID               string   `yaml:"id"`      // 稳定标识，前端生成
	Name             string   `yaml:"name"`    // 展示名（模型前缀用）
	Preset           string   `yaml:"preset"`
	OpenAIBaseURL    string   `yaml:"openai_base_url"`
	AnthropicBaseURL string   `yaml:"anthropic_base_url"`
	APIKey           string   `yaml:"api_key"`
	Models           []string `yaml:"models"`
}

// ModelConfig 是模型平台配置集合 + 默认模型选择。
// Preset/OpenAIBaseURL/AnthropicBaseURL/BaseURL/APIKey/Models/Agents 为旧单平台字段：
// 仅读取迁移用（归一化时迁入 Platforms 并清空），保存时 omitempty 不再落盘。
type ModelConfig struct {
	Enabled         bool            `yaml:"enabled"`
	Platforms       []ModelPlatform `yaml:"platforms"`
	DefaultPlatform string          `yaml:"default_platform"` // 平台 ID；空=第一个
	// legacy 单平台字段：读取后迁移进 Platforms，归一化后清空。
	Preset           string            `yaml:"preset,omitempty"`
	OpenAIBaseURL    string            `yaml:"openai_base_url,omitempty"`
	AnthropicBaseURL string            `yaml:"anthropic_base_url,omitempty"`
	BaseURL          string            `yaml:"base_url,omitempty"`
	APIKey           string            `yaml:"api_key,omitempty"`
	Models           []string          `yaml:"models,omitempty"`
	ActiveModel      string            `yaml:"active_model"` // 默认模型；空=不注入模型名
	Agents           map[string]string `yaml:"agents,omitempty"`
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
	// ProjectSetupDismissed 为 true 后不再自动弹出首次项目选择弹窗。
	ProjectSetupDismissed bool `yaml:"project_setup_dismissed"`
	// Weixin 是微信桥（iLink）的持久化配置。
	Weixin WeixinConfig `yaml:"weixin"`
	// AgentHub 是 AgentHub 的跨端持久化配置（默认工具/模型）。
	AgentHub AgentHubConfig `yaml:"agenthub"`
}

// WeixinConfig 是微信桥（iLink）的持久化配置。
type WeixinConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"` // iLink bot_token（扫码后获得）
	Wxid    string `yaml:"wxid"`  // 白名单用户（首扫即绑）
}

// AgentHubConfig 是 AgentHub 的跨端持久化配置：前端「默认 agent 工具 + 模型」
// 写穿到这里，微信桥等非浏览器端才能读到（localStorage 只存在于桌面端浏览器上下文）。
type AgentHubConfig struct {
	DefaultTool  string `yaml:"default_tool"`  // 快速新建（#en / easynew）默认工具 ID；空=回落第一个已安装
	DefaultModel string `yaml:"default_model"` // 预留：默认模型 ID（当前新建链路用工具默认模型）
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
		Model:          ModelConfig{Models: []string{}, Platforms: []ModelPlatform{}},
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

// Save 原子写配置（atomfile：唯一临时文件 + 包级互斥，防写一半损坏与并发互踩）。
func Save(p Layout, c Config) error {
	if err := EnsureRoot(p); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return atomfile.Write(p.Config, data, 0o755)
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
	// 界面语言：空值或非法值（不匹配 locale 码模式）回落默认（en）。
	if !ValidLanguage(c.Language) {
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
	// 模型配置：统一模型列表归一；旧 base_url / agents 迁移后清空。
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
	// 旧 agents 值去重迁移进 Models（仅当 Models 尚未配置）；
	// map 遍历无序，先排序保证迁移结果与 ActiveModel 取值确定。
	if len(c.Model.Models) == 0 {
		vals := make([]string, 0, len(c.Model.Agents))
		for _, v := range c.Model.Agents {
			if v = strings.TrimSpace(v); v != "" {
				vals = append(vals, v)
			}
		}
		sort.Strings(vals)
		for _, v := range vals {
			if len(c.Model.Models) == 0 || c.Model.Models[len(c.Model.Models)-1] != v {
				c.Model.Models = append(c.Model.Models, v)
			}
		}
		if len(c.Model.Models) > 0 && c.Model.ActiveModel == "" {
			c.Model.ActiveModel = c.Model.Models[0]
		}
	}
	c.Model.Agents = nil
	c.Model.Models = normalizeModels(c.Model.Models)
	c.Model.ActiveModel = strings.TrimSpace(c.Model.ActiveModel)

	// 旧单平台字段迁移：Platforms 为空且 legacy 有任一实质内容时，
	// 打包成一个 default 平台（Name 用 Preset 展示名，缺省「默认平台」）。
	if len(c.Model.Platforms) == 0 &&
		(c.Model.Preset != "" || c.Model.OpenAIBaseURL != "" || c.Model.AnthropicBaseURL != "" ||
			c.Model.APIKey != "" || len(c.Model.Models) > 0) {
		name := c.Model.Preset
		if name == "" {
			name = "默认平台"
		}
		c.Model.Platforms = []ModelPlatform{{
			ID:               "default",
			Name:             name,
			Preset:           c.Model.Preset,
			OpenAIBaseURL:    c.Model.OpenAIBaseURL,
			AnthropicBaseURL: c.Model.AnthropicBaseURL,
			APIKey:           c.Model.APIKey,
			Models:           c.Model.Models,
		}}
		c.Model.DefaultPlatform = "default"
		// ActiveModel 原样保留：语义不变，属于默认平台域。
		// Models 已随平台打包，顶层清掉（末尾统一置空 slice）。
		c.Model.Models = nil
	}

	// 平台归一化：先收集既有显式 ID，再为缺失 ID 按序补 p1/p2…（冲突顺延跳号）；
	// 端点与模型逐平台归一。
	used := map[string]bool{}
	for i := range c.Model.Platforms {
		p := &c.Model.Platforms[i]
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		if p.ID != "" {
			used[p.ID] = true
		}
	}
	n := 0
	for i := range c.Model.Platforms {
		p := &c.Model.Platforms[i]
		if p.ID == "" {
			for {
				n++
				id := fmt.Sprintf("p%d", n)
				if !used[id] {
					p.ID = id
					used[id] = true
					break
				}
			}
		}
		p.OpenAIBaseURL = sanitizeHTTPURL(p.OpenAIBaseURL)
		p.AnthropicBaseURL = sanitizeHTTPURL(p.AnthropicBaseURL)
		p.Models = normalizeModels(p.Models)
	}
	if c.Model.Platforms == nil {
		c.Model.Platforms = []ModelPlatform{}
	}

	// DefaultPlatform 校验：指向不存在的平台时回落第一个；无平台则连同 ActiveModel 清空。
	c.Model.DefaultPlatform = strings.TrimSpace(c.Model.DefaultPlatform)
	if c.Model.DefaultPlatform != "" {
		found := false
		for _, p := range c.Model.Platforms {
			if p.ID == c.Model.DefaultPlatform {
				found = true
				break
			}
		}
		if !found {
			if len(c.Model.Platforms) > 0 {
				c.Model.DefaultPlatform = c.Model.Platforms[0].ID
			} else {
				c.Model.DefaultPlatform = ""
				c.Model.ActiveModel = ""
			}
		}
	}

	// legacy 字段已完成迁移/归一，全部清空（保存时 omitempty 不再落盘）。
	// Models 保持非 nil 空 slice，与 Default() 及旧版归一化行为一致。
	c.Model.Preset = ""
	c.Model.OpenAIBaseURL = ""
	c.Model.AnthropicBaseURL = ""
	c.Model.APIKey = ""
	if c.Model.Models == nil {
		c.Model.Models = []string{}
	}
	return c
}

// ActivePlatform 返回默认平台（DefaultPlatform 命中，否则第一个）。
// 无平台时返回 false。
func (m ModelConfig) ActivePlatform() (ModelPlatform, bool) {
	if len(m.Platforms) == 0 {
		return ModelPlatform{}, false
	}
	if m.DefaultPlatform != "" {
		for _, p := range m.Platforms {
			if p.ID == m.DefaultPlatform {
				return p, true
			}
		}
	}
	return m.Platforms[0], true
}

// normalizeModels 对模型 ID 列表 TrimSpace、去空串、去重并保持顺序；返回非 nil slice。
func normalizeModels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
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
