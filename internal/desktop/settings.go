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

// ModelPlatformView 是回传前端的单平台视图：不含密钥明文。
type ModelPlatformView struct {
	ID               string   `json:"ID"`
	Name             string   `json:"Name"`
	Preset           string   `json:"Preset"`
	OpenAIBaseURL    string   `json:"OpenAIBaseURL"`
	AnthropicBaseURL string   `json:"AnthropicBaseURL"`
	Models           []string `json:"Models"`
	APIKeySet        bool     `json:"APIKeySet"`
	IsDefault        bool     `json:"IsDefault"`
}

// ModelConfigView 是回传前端的模型配置视图。
type ModelConfigView struct {
	Enabled         bool                `json:"Enabled"`
	Platforms       []ModelPlatformView `json:"Platforms"`
	DefaultPlatform string              `json:"DefaultPlatform"`
	ActiveModel     string              `json:"ActiveModel"`
}

// ModelPlatformInput 是前端提交的单平台：APIKey 空串=保持原值（仅对已存在平台），ClearAPIKey 显式清除。
type ModelPlatformInput struct {
	ID               string   `json:"ID"`
	Name             string   `json:"Name"`
	Preset           string   `json:"Preset"`
	OpenAIBaseURL    string   `json:"OpenAIBaseURL"`
	AnthropicBaseURL string   `json:"AnthropicBaseURL"`
	APIKey           string   `json:"APIKey"`
	ClearAPIKey      bool     `json:"ClearAPIKey"`
	Models           []string `json:"Models"`
}

// ModelConfigInput 是前端提交的模型配置：整体保存语义。
type ModelConfigInput struct {
	Enabled         bool                 `json:"Enabled"`
	Platforms       []ModelPlatformInput `json:"Platforms"`
	DefaultPlatform string               `json:"DefaultPlatform"`
	ActiveModel     string               `json:"ActiveModel"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	views := make([]ModelPlatformView, 0, len(m.Platforms))
	for i, p := range m.Platforms {
		models := p.Models
		if models == nil {
			models = []string{}
		}
		// 默认平台未显式指定时与 ActivePlatform() 一致回落第一个。
		views = append(views, ModelPlatformView{
			ID:               p.ID,
			Name:             p.Name,
			Preset:           p.Preset,
			OpenAIBaseURL:    p.OpenAIBaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			Models:           models,
			APIKeySet:        p.APIKey != "",
			IsDefault:        p.ID == m.DefaultPlatform || (m.DefaultPlatform == "" && i == 0),
		})
	}
	return ModelConfigView{
		Enabled:         m.Enabled,
		Platforms:       views,
		DefaultPlatform: m.DefaultPlatform,
		ActiveModel:     m.ActiveModel,
	}, nil
}

// cleanModels 对模型 ID 列表 TrimSpace、去空串、去重并保持顺序。
func cleanModels(in []string) []string {
	models := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		models = append(models, v)
	}
	return models
}

// SetModelConfig 整包保存模型平台配置：输入列表即最终平台列表，
// 未出现在输入中的既有平台视为删除；已存在平台 APIKey 空串=保留原值。
func (a *App) SetModelConfig(in ModelConfigInput) error {
	old := make(map[string]config.ModelPlatform, len(a.snapshot().Config.Model.Platforms))
	for _, p := range a.snapshot().Config.Model.Platforms {
		old[p.ID] = p
	}

	platforms := make([]config.ModelPlatform, 0, len(in.Platforms))
	seen := map[string]bool{}
	for _, pi := range in.Platforms {
		name := strings.TrimSpace(pi.Name)
		if name == "" {
			return errors.New("err.settings.model_platform_name_required")
		}
		id := strings.TrimSpace(pi.ID)
		if id == "" {
			return errors.New("err.settings.model_platform_id_required")
		}
		if seen[id] {
			return errors.New("err.settings.model_platform_id_duplicated")
		}
		seen[id] = true
		oai := strings.TrimSpace(pi.OpenAIBaseURL)
		ant := strings.TrimSpace(pi.AnthropicBaseURL)
		if err := validateHTTPURL(oai, fmt.Sprintf("%s OpenAI Base URL", name)); err != nil {
			return err
		}
		if err := validateHTTPURL(ant, fmt.Sprintf("%s Anthropic Base URL", name)); err != nil {
			return err
		}
		p := config.ModelPlatform{
			ID:               id,
			Name:             name,
			Preset:           strings.TrimSpace(pi.Preset),
			OpenAIBaseURL:    oai,
			AnthropicBaseURL: ant,
			APIKey:           pi.APIKey,
			Models:           cleanModels(pi.Models),
		}
		if prev, ok := old[id]; ok {
			switch {
			case pi.ClearAPIKey:
				p.APIKey = ""
			case pi.APIKey == "":
				p.APIKey = prev.APIKey
			}
		}
		platforms = append(platforms, p)
	}

	// DefaultPlatform 必须命中某个平台，否则回落第一个；无平台则置空。
	def := strings.TrimSpace(in.DefaultPlatform)
	switch {
	case def != "" && seen[def]:
	case len(platforms) > 0:
		def = platforms[0].ID
	default:
		def = ""
	}

	return a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.Platforms = platforms
		c.Model.DefaultPlatform = def
		c.Model.ActiveModel = strings.TrimSpace(in.ActiveModel)
		// 保存时清空全部 legacy 单平台字段，避免 Load 再迁移。
		c.Model.Preset = ""
		c.Model.OpenAIBaseURL = ""
		c.Model.AnthropicBaseURL = ""
		c.Model.BaseURL = ""
		c.Model.APIKey = ""
		c.Model.Models = nil
		c.Model.Agents = nil
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
