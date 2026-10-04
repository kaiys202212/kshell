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

var errNoProvidersPath = errors.New("providers.yaml 路径不可用（未初始化完成）")

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

// SaveProvidersYAML 保存自定义工具定义：先解析校验（合法但内容为空也允许），
// 再临时文件原子替换写回，避免写一半崩溃损坏配置。
// 写盘成功后热重载 provider 表并刷新工具检测，无需重启。
func (a *App) SaveProvidersYAML(content string) error {
	path := a.snapshot().ProvidersPath
	if path == "" {
		return errNoProvidersPath
	}
	if _, err := providers.ParseProvidersYAML([]byte(content)); err != nil {
		return errors.New("YAML 解析失败：" + err.Error())
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
	Enabled          bool              `json:"Enabled"`
	Preset           string            `json:"Preset"`
	OpenAIBaseURL    string            `json:"OpenAIBaseURL"`
	AnthropicBaseURL string            `json:"AnthropicBaseURL"`
	Agents           map[string]string `json:"Agents"`
	APIKeySet        bool              `json:"APIKeySet"`
}

// ModelConfigInput 是前端提交的模型配置：APIKey 空串=保持原值，ClearAPIKey 显式清除。
type ModelConfigInput struct {
	Enabled          bool              `json:"Enabled"`
	Preset           string            `json:"Preset"`
	OpenAIBaseURL    string            `json:"OpenAIBaseURL"`
	AnthropicBaseURL string            `json:"AnthropicBaseURL"`
	APIKey           string            `json:"APIKey"`
	ClearAPIKey      bool              `json:"ClearAPIKey"`
	Agents           map[string]string `json:"Agents"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	agents := m.Agents
	if agents == nil {
		agents = map[string]string{}
	}
	return ModelConfigView{
		Enabled:          m.Enabled,
		Preset:           m.Preset,
		OpenAIBaseURL:    m.OpenAIBaseURL,
		AnthropicBaseURL: m.AnthropicBaseURL,
		Agents:           agents,
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
	agents := map[string]string{}
	for k, v := range in.Agents {
		agents[k] = strings.TrimSpace(v)
	}
	return a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.Preset = strings.TrimSpace(in.Preset)
		c.Model.OpenAIBaseURL = oai
		c.Model.AnthropicBaseURL = ant
		c.Model.BaseURL = ""
		c.Model.Agents = agents
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
		return fmt.Errorf("%s 必须以 http:// 或 https:// 开头", label)
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
		return fmt.Errorf("无效的会话模式：%s", mode)
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
		return fmt.Errorf("无效的权限模式：%s", mode)
	}
	return a.saveConfig(func(c *config.Config) { c.PermissionMode = mode })
}
