package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

var errNoProvidersPath = errors.New("err.settings.providers_path_unavailable")

// GetTools 返回最近一次扫描的工具安装状态（含未安装项，前端灰显）。
// 首轮扫描尚未完成时等一轮（上限 scanReadyTimeout）：前端工作区页签只在挂载时取一次
// 工具，若此刻返回空列表，「选择 agent」下拉会一直禁用（表现为“没有可选项”）。
func (a *App) GetTools() []discovery.Tool {
	a.ensureToolsReady()

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tools == nil {
		return []discovery.Tool{}
	}
	return a.tools
}

// CheckToolAuth 专供登录弹窗轮询：按 toolID 找到 provider 并实时探测登录态。
// 刻意绕过 a.tools（其中快照灌入/上轮扫描的 Auth 都可能是历史结论），每次调用
// 都直接走 providers.AuthProber 实探。与 GetTools 同源：先 ensureToolsReady 等
// provider 表装配完成，再经 snapshot() 加锁读取。
// 未实现 AuthProber 或 toolID 未知时返回 AuthUnknown 与 nil 错误——前端轮询
// 逻辑据此继续等待，不当作异常打断弹窗流程。
func (a *App) CheckToolAuth(toolID string) (string, error) {
	a.ensureToolsReady()

	o := a.snapshot()
	for _, p := range o.Providers {
		if p.ID() != toolID {
			continue
		}
		if ap, ok := p.(providers.AuthProber); ok {
			return string(ap.ProbeAuth()), nil
		}
		return string(providers.AuthUnknown), nil
	}
	return string(providers.AuthUnknown), nil
}

// SaveProvidersYAML 保存自定义工具定义：先解析校验（合法但内容为空也允许），
// 再临时文件原子替换写回，避免写一半崩溃损坏配置。
// 写盘成功后热重载 provider 表并刷新工具检测，无需重启。
func (a *App) SaveProvidersYAML(content string) error {
	path := a.snapshot().ProvidersPath
	if path == "" {
		return errNoProvidersPath
	}
	if _, err := providers.ParseProvidersYAML([]byte(content)); err != nil {
		return fmt.Errorf("err.settings.yaml_parse_failed|%w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 每次用唯一临时文件：并发保存不会交错写同一个 tmp
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后残留清理是空操作
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	a.reloadProviders()
	return nil
}

// reloadProviders 按磁盘 yaml 重新装配运行中的 provider 表并刷新工具检测。
func (a *App) reloadProviders() {
	o := a.snapshot()
	if o.ProvidersPath == "" || o.Home == "" {
		return
	}
	specs, err := providers.LoadGenericSpecs(o.ProvidersPath)
	if err != nil {
		specs = nil
	}
	ps := providers.MergeProviders(providers.Builtins(), specs, o.Home)
	a.mu.Lock()
	a.opts.Providers = ps
	a.mu.Unlock()
	a.publishDetectedTools(discovery.DetectAll(o.Home, ps))
	_, _ = a.ScanSessions()
}

// ParseProvidersYAML 供设置页源码→表单。
func (a *App) ParseProvidersYAML(content string) ([]providers.GenericSpec, error) {
	return providers.ParseProvidersYAML([]byte(content))
}

// FormatProvidersYAML 供设置页表单→源码。
func (a *App) FormatProvidersYAML(specs []providers.GenericSpec) (string, error) {
	return providers.FormatProvidersYAML(specs)
}

// LoadProvidersYAML 读出当前自定义工具定义全文，供设置页回填编辑器。
func (a *App) LoadProvidersYAML() (string, error) {
	path := a.snapshot().ProvidersPath
	if path == "" {
		return "", errNoProvidersPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return providers.DefaultProvidersYAML(), nil // 文件缺失时回填模板
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ModelConfigView 是回传前端的模型配置视图：绝不包含密钥明文。
type ModelConfigView struct {
	Enabled          bool     `json:"Enabled"`
	Preset           string   `json:"Preset"`
	OpenAIBaseURL    string   `json:"OpenAIBaseURL"`
	AnthropicBaseURL string   `json:"AnthropicBaseURL"`
	Models           []string `json:"Models"`
	ActiveModel      string   `json:"ActiveModel"`
	APIKeySet        bool     `json:"APIKeySet"`
}

// ModelConfigInput 是前端提交的模型配置：APIKey 空串=保持原值，ClearAPIKey 显式清除。
type ModelConfigInput struct {
	Enabled          bool     `json:"Enabled"`
	Preset           string   `json:"Preset"`
	OpenAIBaseURL    string   `json:"OpenAIBaseURL"`
	AnthropicBaseURL string   `json:"AnthropicBaseURL"`
	APIKey           string   `json:"APIKey"`
	ClearAPIKey      bool     `json:"ClearAPIKey"`
	Models           []string `json:"Models"`
	ActiveModel      string   `json:"ActiveModel"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	models := m.Models
	if models == nil {
		models = []string{}
	}
	return ModelConfigView{
		Enabled:          m.Enabled,
		Preset:           m.Preset,
		OpenAIBaseURL:    m.OpenAIBaseURL,
		AnthropicBaseURL: m.AnthropicBaseURL,
		Models:           models,
		ActiveModel:      m.ActiveModel,
		APIKeySet:        m.APIKey != "",
	}, nil
}

func (a *App) SetModelConfig(in ModelConfigInput) error {
	oai := strings.TrimSpace(in.OpenAIBaseURL)
	ant := strings.TrimSpace(in.AnthropicBaseURL)
	if err := validateHTTPURL(oai, "OpenAI Base URL"); err != nil {
		return err
	}
	if err := validateHTTPURL(ant, "Anthropic Base URL"); err != nil {
		return err
	}
	models := make([]string, 0, len(in.Models))
	seen := map[string]bool{}
	for _, v := range in.Models {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		models = append(models, v)
	}
	return a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.Preset = strings.TrimSpace(in.Preset)
		c.Model.OpenAIBaseURL = oai
		c.Model.AnthropicBaseURL = ant
		c.Model.BaseURL = ""
		c.Model.Models = models
		c.Model.ActiveModel = strings.TrimSpace(in.ActiveModel)
		switch {
		case in.ClearAPIKey:
			c.Model.APIKey = ""
		case in.APIKey != "":
			c.Model.APIKey = in.APIKey
		}
	})
}

func validateHTTPURL(u, label string) error {
	if u == "" {
		return nil
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("err.settings.http_url_required|%s", label)
	}
	return nil
}

// ListModelPresets 返回内置模型提供商预设。
func (a *App) ListModelPresets() []providers.ModelPreset {
	return providers.ModelPresets()
}

// GetSessionMode 返回默认会话打开路径（tui | acp）。
func (a *App) GetSessionMode() string {
	return a.snapshot().Config.SessionMode
}

// SetSessionMode 保存会话模式偏好。
func (a *App) SetSessionMode(mode string) error {
	switch mode {
	case config.SessionModeTUI, config.SessionModeACP:
	default:
		return fmt.Errorf("err.settings.invalid_session_mode|%s", mode)
	}
	return a.saveConfig(func(c *config.Config) { c.SessionMode = mode })
}

// GetPermissionMode 返回权限模式（default | bypass）。
func (a *App) GetPermissionMode() string {
	return a.snapshot().Config.PermissionMode
}

// SetPermissionMode 保存权限模式。
func (a *App) SetPermissionMode(mode string) error {
	switch mode {
	case config.PermissionModeDefault, config.PermissionModeBypass:
	default:
		return fmt.Errorf("err.settings.invalid_permission_mode|%s", mode)
	}
	return a.saveConfig(func(c *config.Config) { c.PermissionMode = mode })
}
