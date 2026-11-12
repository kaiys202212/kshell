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
	"github.com/yangk/kshell/internal/version"
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
	ID               string   `yaml:"id"`   // 稳定标识，前端生成
	Name             string   `yaml:"name"` // 展示名（模型前缀用）
	Preset           string   `yaml:"preset"`
	OpenAIBaseURL    string   `yaml:"openai_base_url"`
	AnthropicBaseURL string   `yaml:"anthropic_base_url"`
	APIKey           string   `yaml:"api_key"`
	Models           []string `yaml:"models"`
	// ASRModel 是可选的语音转写模型（OpenAI 兼容 /audio/transcriptions 端点），
	// 供微信桥把无转写的语音转成文字；空 = 该平台不做语音转写。
	ASRModel string `yaml:"asr_model,omitempty"`
}

// ClaudeSync 是 Claude Code 静态注入的 4 类模型槽位映射：每槽取
// ToolPlatforms["claude"] 选定平台的模型 ID；空 = 不写对应 env 键（同步先删后建自然清掉）。
type ClaudeSync struct {
	Model  string `yaml:"model,omitempty"`  // ANTHROPIC_MODEL（默认模型槽）
	Sonnet string `yaml:"sonnet,omitempty"` // ANTHROPIC_DEFAULT_SONNET_MODEL
	Opus   string `yaml:"opus,omitempty"`   // ANTHROPIC_DEFAULT_OPUS_MODEL
	Haiku  string `yaml:"haiku,omitempty"`  // ANTHROPIC_DEFAULT_HAIKU_MODEL
}

// ToolSyncConfig 是按工具定制的模型同步配置：每个同步目标（opencode/codebuddy/
// claude）独立启用并勾选注入模型。未启用或未勾选任何模型 = 只清不建。
type ToolSyncConfig struct {
	// Enabled 每工具启用开关，只存 true 项（缺省即禁用，禁用 = 删键）。
	// 无 omitempty：空 map 也要落盘为 `enabled: {}`，与「文件里没有 tool_sync
	// 键」区分——后者才会触发旧 sync_off_tools 的载入迁移，前者是用户的显式
	// 全禁用状态，不能被迁移重新打开。
	Enabled map[string]bool `yaml:"enabled"`
	Claude  ClaudeSync      `yaml:"claude,omitempty"`
	// ToolPlatforms 是每工具静态注入/端点解析的平台 ID 映射（toolID → 平台 ID）。
	// 取代旧 ClaudePlatform/CodeBuddyPlatform 单字段；空/悬空回落第一个平台。
	ToolPlatforms map[string]string `yaml:"tool_platforms,omitempty"`
	// OpenCode 平台 ID → 勾选的模型 ID；无勾选的平台不建 provider。
	OpenCode map[string][]string `yaml:"opencode,omitempty"`
	// CodeBuddy 是 ToolPlatforms["codebuddy"] 选定平台勾选的模型 ID。
	CodeBuddy []string `yaml:"codebuddy,omitempty"`
	// legacy：仅读取迁移用，归一化时迁入 ToolPlatforms 并清空，保存不再落盘。
	ClaudePlatform    string `yaml:"claude_platform,omitempty"`
	CodeBuddyPlatform string `yaml:"codebuddy_platform,omitempty"`
}

// ModelConfig 是模型平台配置集合 + kshell 当前平台/模型选择。
// Preset/OpenAIBaseURL/AnthropicBaseURL/BaseURL/APIKey/Models/Agents 为旧单平台字段：
// 仅读取迁移用（归一化时迁入 Platforms 并清空），保存时 omitempty 不再落盘。
type ModelConfig struct {
	Enabled   bool            `yaml:"enabled"`
	Platforms []ModelPlatform `yaml:"platforms"`
	// ActivePlatformID 是 kshell 自身使用的平台，与 ActiveModel 成对；空=第一个。
	ActivePlatformID string `yaml:"active_platform"`
	// ToolSync 是按工具定制的模型同步配置（取代旧 sync_off_tools 总开关语义）。
	ToolSync ToolSyncConfig `yaml:"tool_sync"`
	// legacy 单平台字段：读取后迁移进 Platforms，归一化后清空。
	Preset           string   `yaml:"preset,omitempty"`
	OpenAIBaseURL    string   `yaml:"openai_base_url,omitempty"`
	AnthropicBaseURL string   `yaml:"anthropic_base_url,omitempty"`
	BaseURL          string   `yaml:"base_url,omitempty"`
	APIKey           string   `yaml:"api_key,omitempty"`
	Models           []string `yaml:"models,omitempty"`
	// ActiveModel 是 kshell 自身默认模型；仅供 hub 自主工具/微信 ASR 等自身
	// LLM 调用，不注入给 agent；语义上属于 ActivePlatformID。
	ActiveModel string `yaml:"active_model"`
	// DefaultPlatform 是旧全局默认平台：仅读取迁移，归一化后清空、不再落盘。
	DefaultPlatform string `yaml:"default_platform,omitempty"`
	// SyncOffTools 是旧版「按工具关同步」列表：仅读取迁移用（归一化时迁入
	// ToolSync.Enabled 并清空），保存时 omitempty 不再落盘。
	SyncOffTools []string          `yaml:"sync_off_tools,omitempty"`
	Agents       map[string]string `yaml:"agents,omitempty"`
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
	// LastSeenVersion 记录用户已确认过的最新版本号（升级后弹升级日志的去重依据）。
	LastSeenVersion string `yaml:"last_seen_version"`
	// Autostart 开机自启动开关：发布版缺省开、dev 构建缺省关（见 isReleaseFn）。
	// 缺省值只在 config.yaml 缺字段时生效——显式 false 必须保留，因此解码时
	// 用 autostartSet 区分「未写字段」与「写了 false」（见 UnmarshalYAML）。
	Autostart bool `yaml:"autostart"`

	// autostartSet 由 UnmarshalYAML 置位：config.yaml 显式写过 autostart 键。
	// 不落盘（yaml 忽略未导出字段），仅归一化时用于决定是否补缺省值。
	autostartSet bool
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

// AgentHub 路由模式：无目标自由文本是直接投递还是先经本地模型挑目标。
// 精确目标（@会话 / 编号选择 / 引用回复 / ACP 窗口）永远直投，不受此开关影响。
const (
	RouteModeAuto  = "auto"  // 默认：先问意图路由，路由不接管才回落待选/直投
	RouteModeAgent = "agent" // 关闭智能路由：无目标自由文本一律进待选让用户选
)

// routeModeForceAutoLegacy 是已下线的历史取值：它原先表示「跳过意图判定直接进
// 工具循环」，工具循环删除后语义消失，归一化时按 auto 处理（旧配置因此自动兼容）。
const routeModeForceAutoLegacy = "force_auto"

// AgentHub 编排参数缺省值：心跳阈值默认 5 分钟、上限 120 分钟。
const (
	agenthubHeartbeatDefault = 5
	agenthubHeartbeatMax     = 120
)

// AgentHubConfig 是 AgentHub 的跨端持久化配置：前端「默认 agent 工具 + 模型」
// 写穿到这里，微信桥等非浏览器端才能读到（localStorage 只存在于桌面端浏览器上下文）。
type AgentHubConfig struct {
	DefaultTool  string `yaml:"default_tool"`  // 快速新建（#en / easynew）默认工具 ID；空=回落第一个已安装
	DefaultModel string `yaml:"default_model"` // 快速新建默认模型 ID；空=工具自身默认；微信 EasyNew / 桌面写穿共用
	// RouteMode 是输入路由模式：auto（默认）| agent | force_auto。
	RouteMode string `yaml:"route_mode"`
	// Enabled 是 AgentHub 总开关：nil（yaml 未写）=启用，显式 false=关闭。
	// 关闭时 Hub 面板收起、微信扫码入口隐藏、微信桥不启动轮询。
	Enabled *bool `yaml:"enabled,omitempty"`
	// HeartbeatMinutes 是长任务心跳阈值（分钟）：聚合模式下会话持续 running 超过该
	// 时长中途插一行提示；<=0 视为未配置归一为 5，上限 120。
	HeartbeatMinutes int `yaml:"heartbeat_minutes,omitempty"`
	// DirectPatterns 是逃生门白名单（前缀/首词匹配，智能路由不接管这些输入）。
	// 空/nil = 使用 hub 包内置默认集（# 命令/@/continue/explain 由消费方定义）。
	DirectPatterns []string `yaml:"direct_patterns,omitempty"`
}

// EnabledOn 返回总开关的生效值：nil 即启用（对存量配置保持现状）。
func (c AgentHubConfig) EnabledOn() bool { return c.Enabled == nil || *c.Enabled }

// Normalized 归一化 AgentHub 配置：路由模式非法/已下线回落 auto、心跳阈值钳默认/上限、
// 逃生门白名单去空白项去重（全空置 nil）。供 Config.normalized 与写穿保存共用。
func (c AgentHubConfig) Normalized() AgentHubConfig {
	// 路由模式：空值或非法值一律回落 auto；已下线的 force_auto 同样按 auto 处理。
	c.RouteMode = strings.TrimSpace(c.RouteMode)
	switch c.RouteMode {
	case RouteModeAuto, RouteModeAgent:
	case routeModeForceAutoLegacy:
		c.RouteMode = RouteModeAuto
	default:
		c.RouteMode = RouteModeAuto
	}
	// 编排参数：心跳阈值钳制、逃生门白名单去空白项去重。
	if c.HeartbeatMinutes <= 0 {
		c.HeartbeatMinutes = agenthubHeartbeatDefault
	}
	if c.HeartbeatMinutes > agenthubHeartbeatMax {
		c.HeartbeatMinutes = agenthubHeartbeatMax
	}
	if len(c.DirectPatterns) > 0 {
		seen := map[string]bool{}
		pats := make([]string, 0, len(c.DirectPatterns))
		for _, p := range c.DirectPatterns {
			if p = strings.TrimSpace(p); p == "" || seen[p] {
				continue
			}
			seen[p] = true
			pats = append(pats, p)
		}
		if len(pats) == 0 {
			c.DirectPatterns = nil
		} else {
			c.DirectPatterns = pats
		}
	}
	return c
}

// isReleaseFn 是发布态判定的包级变量：默认走 version.IsRelease，测试可注入。
var isReleaseFn = version.IsRelease

// defaultAutostart 返回 autostart 缺省值：发布版默认开启（需求确认的形态），
// dev 构建不开启也不注册。
func defaultAutostart(release bool) bool { return release }

// UnmarshalYAML 在标准解码前检测 autostart 键是否显式出现：bool 零值无法区分
// 「未写字段」与「写了 false」，未写时置位标记交给 normalized 补缺省值，
// 显式写的值（含 false）随后被别名类型解码原样覆盖保留。
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "autostart" {
			c.autostartSet = true
			break
		}
	}
	type plain Config
	return node.Decode((*plain)(c))
}

func Default() Config {
	return Config{
		Autostart: defaultAutostart(isReleaseFn()),
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
		AgentHub:       AgentHubConfig{RouteMode: RouteModeAuto, HeartbeatMinutes: agenthubHeartbeatDefault},
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
	// 已读版本号：仅去首尾空白，其余原样保留。
	c.LastSeenVersion = strings.TrimSpace(c.LastSeenVersion)
	// 开机自启动：config.yaml 未写该字段时补发布态缺省值（显式写过的 false/true 原样保留）。
	// 直接取 d.Autostart（Default() 已按 isReleaseFn 算过），保证两处缺省同源。
	if !c.autostartSet {
		c.Autostart = d.Autostart
	}
	// 消费完即复位：归一化产物（Load 返回值）与手构造的 Config 保持可 DeepEqual；
	// normalized 只在 Load 末尾调用一次，复位不影响显式 false 的保留语义。
	c.autostartSet = false
	// AgentHub 路由模式/编排参数归一（抽为 AgentHubConfig.Normalized 共用）。
	c.AgentHub = c.AgentHub.Normalized()
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
	// 旧单工具平台字段迁移为 ToolPlatforms 映射：只有映射尚未出现（nil）时才迁移，
	// 已有新键时以新键为准（不合并旧值）。迁移后旧字段统一清空，保存只落新格式。
	ts := &c.Model.ToolSync
	// len==0 而非 ==nil：显式空映射（tool_platforms: {}）也视为「尚未配置」，
	// 否则升级时旧键会被空映射挡住而丢数据。逐 key 补缺，已有新键不覆盖。
	if len(ts.ToolPlatforms) == 0 {
		ts.ToolPlatforms = nil
	}
	for _, legacy := range []struct{ tool, id string }{
		{"claude", strings.TrimSpace(ts.ClaudePlatform)},
		{"codebuddy", strings.TrimSpace(ts.CodeBuddyPlatform)},
	} {
		if legacy.id == "" {
			continue
		}
		if ts.ToolPlatforms == nil {
			ts.ToolPlatforms = map[string]string{}
		}
		if _, ok := ts.ToolPlatforms[legacy.tool]; !ok {
			ts.ToolPlatforms[legacy.tool] = legacy.id
		}
	}
	ts.ClaudePlatform = ""
	ts.CodeBuddyPlatform = ""
	// 旧 sync_off_tools 迁移为 ToolSync.Enabled：全局启用过模型同步时，未列出
	// 的目标视为开启、列出的视为关闭；从未启用过则保持空 map（= 全部禁用）。
	// Enabled 只存 true 项（缺省即禁用），禁用 = 删键。文件里已有 tool_sync
	//（Enabled 非 nil 即算显式配置）时以新键为准、不再迁移。
	// 迁移后旧字段清空，保存时 omitempty 不再落盘。
	tsZero := ts.Enabled == nil &&
		ts.Claude == ClaudeSync{} &&
		len(ts.ToolPlatforms) == 0 &&
		len(ts.OpenCode) == 0 &&
		len(ts.CodeBuddy) == 0
	if tsZero && (c.Model.SyncOffTools != nil || c.Model.Enabled) {
		off := map[string]bool{}
		for _, id := range c.Model.SyncOffTools {
			if id = strings.TrimSpace(id); id != "" {
				off[id] = true
			}
		}
		if c.Model.Enabled {
			c.Model.ToolSync.Enabled = map[string]bool{}
			for _, tool := range []string{"opencode", "codebuddy", "claude"} {
				if !off[tool] {
					c.Model.ToolSync.Enabled[tool] = true
				}
			}
		}
	}
	c.Model.SyncOffTools = nil

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
		p.ASRModel = strings.TrimSpace(p.ASRModel)
	}
	if c.Model.Platforms == nil {
		c.Model.Platforms = []ModelPlatform{}
	}

	// ActivePlatformID 迁移：旧 default_platform 优先，其次校验悬空回落第一个。
	c.Model.ActivePlatformID = strings.TrimSpace(c.Model.ActivePlatformID)
	legacyDefault := strings.TrimSpace(c.Model.DefaultPlatform)
	if c.Model.ActivePlatformID == "" {
		c.Model.ActivePlatformID = legacyDefault
	}
	if c.Model.ActivePlatformID != "" {
		found := false
		for _, p := range c.Model.Platforms {
			if p.ID == c.Model.ActivePlatformID {
				found = true
				break
			}
		}
		if !found {
			if len(c.Model.Platforms) > 0 {
				c.Model.ActivePlatformID = c.Model.Platforms[0].ID
			} else {
				c.Model.ActivePlatformID = ""
			}
		}
	}

	// 工具平台迁移：旧全局默认平台只在对应工具已启用或确有选择时下放，
	// 避免给从未用过的工具写无关平台 ID。Claude 的端点/密钥注入只看 Enabled，
	// 槽位可以为空，因此启用即视为「配置过」，否则升级后会静默回落到第一个平台。
	if legacyDefault != "" {
		if c.Model.ToolSync.ToolPlatforms == nil {
			c.Model.ToolSync.ToolPlatforms = map[string]string{}
		}
		if id, ok := c.Model.ToolSync.ToolPlatforms["claude"]; (!ok || strings.TrimSpace(id) == "") &&
			(c.Model.ToolSync.Claude != (ClaudeSync{}) || c.Model.ToolSync.Enabled["claude"]) {
			c.Model.ToolSync.ToolPlatforms["claude"] = legacyDefault
		}
		if id, ok := c.Model.ToolSync.ToolPlatforms["codebuddy"]; (!ok || strings.TrimSpace(id) == "") &&
			(len(c.Model.ToolSync.CodeBuddy) > 0 || c.Model.ToolSync.Enabled["codebuddy"]) {
			c.Model.ToolSync.ToolPlatforms["codebuddy"] = legacyDefault
		}
	}
	// 工具平台映射归一：key/value 去空白、丢空项。悬空 ID 不在此处校验，
	// 由消费方统一按「空/悬空回落第一个」处理，避免迁移层静默丢用户数据。
	if len(c.Model.ToolSync.ToolPlatforms) > 0 {
		clean := make(map[string]string, len(c.Model.ToolSync.ToolPlatforms))
		for tool, id := range c.Model.ToolSync.ToolPlatforms {
			if tool = strings.TrimSpace(tool); tool == "" {
				continue
			}
			if id = strings.TrimSpace(id); id == "" {
				continue
			}
			clean[tool] = id
		}
		if len(clean) == 0 {
			clean = nil
		}
		c.Model.ToolSync.ToolPlatforms = clean
	}

	// legacy 字段已完成迁移/归一，全部清空（保存时 omitempty 不再落盘）。
	// Models 保持非 nil 空 slice，与 Default() 及旧版归一化行为一致。
	// 无平台时所有选择悬空（端点/密钥都不存在），一并清空：否则 ActiveModel
	// 会作为「kshell 托管过模型」的唯一痕迹，让启动同步反复清掉用户或
	// ccswitch 写入的 ANTHROPIC_*。真正清理历史注入由保存路径用旧配置做一次性同步。
	if len(c.Model.Platforms) == 0 {
		c.Model.ActivePlatformID = ""
		c.Model.ActiveModel = ""
		// 无平台时工具平台选择全部悬空，映射一并清空（消费方本来也无可回落）。
		c.Model.ToolSync.ToolPlatforms = nil
	}
	c.Model.DefaultPlatform = ""
	c.Model.Preset = ""
	c.Model.OpenAIBaseURL = ""
	c.Model.AnthropicBaseURL = ""
	c.Model.APIKey = ""
	if c.Model.Models == nil {
		c.Model.Models = []string{}
	}
	return c
}

// ActivePlatform 返回 kshell 当前平台（ActivePlatformID 命中，否则第一个）。
// 无平台时返回 false。
func (m ModelConfig) ActivePlatform() (ModelPlatform, bool) {
	if p := m.effectivePlatform(); p != nil {
		return *p, true
	}
	return ModelPlatform{}, false
}

// effectivePlatform 返回 ActivePlatformID 命中的平台指针；空/悬空回落第一个。
// 无平台返回 nil。
func (m ModelConfig) effectivePlatform() *ModelPlatform {
	if len(m.Platforms) == 0 {
		return nil
	}
	if m.ActivePlatformID != "" {
		for i := range m.Platforms {
			if m.Platforms[i].ID == m.ActivePlatformID {
				return &m.Platforms[i]
			}
		}
	}
	return &m.Platforms[0]
}

// ToolPlatformID 返回工具选定的平台 ID：优先新映射，旧单字段仅作未归一化
// 配置的兜底。未配置返回空串，由消费方回落第一个平台。
func (t ToolSyncConfig) ToolPlatformID(toolID string) string {
	toolID = strings.TrimSpace(toolID)
	if id := strings.TrimSpace(t.ToolPlatforms[toolID]); id != "" {
		return id
	}
	switch toolID {
	case "claude":
		return strings.TrimSpace(t.ClaudePlatform)
	case "codebuddy":
		return strings.TrimSpace(t.CodeBuddyPlatform)
	}
	return ""
}

// ToolPlatform 返回已知工具选定平台；空/悬空回落第一个，无平台或未知
// toolID 返回 false。已知 = ToolPlatforms 中显式出现的键，或兼容旧字段的
// claude/codebuddy：拼错的 toolID 若静默拿第一个平台，会掩盖调用方 bug。
func (m ModelConfig) ToolPlatform(toolID string) (ModelPlatform, bool) {
	toolID = strings.TrimSpace(toolID)
	id, known := m.ToolSync.ToolPlatforms[toolID]
	if !known {
		switch toolID {
		case "claude", "codebuddy":
			id = m.ToolSync.ToolPlatformID(toolID)
		default:
			return ModelPlatform{}, false
		}
	}
	if len(m.Platforms) == 0 {
		return ModelPlatform{}, false
	}
	if id = strings.TrimSpace(id); id != "" {
		for _, p := range m.Platforms {
			if p.ID == id {
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
