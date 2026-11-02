package desktop

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/agentsync"
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
	// 出口归一化：Auth 三态（in/out/unknown）必须总有值。历史快照灌入或测试
	// 构造的工具可能漏填（空串），前端按 Auth === 'out' 做未登录徽标/降级判断，
	// 空串会被当成「已登录」误判，出口统一补成 unknown。
	tools := append([]discovery.Tool(nil), a.tools...)
	for i := range tools {
		if tools[i].Auth == "" {
			tools[i].Auth = string(providers.AuthUnknown)
		}
	}
	return tools
}

// CheckToolAuth 专供登录弹窗轮询：按 toolID 找到 provider 并实时探测登录态。
// 刻意绕过 a.tools（其中快照灌入/上轮扫描的 Auth 都可能是历史结论），每次调用
// 都直接走 providers.AuthProber 实探。不依赖扫描就绪：provider 表（o.Providers）
// 启动装配即可用，直接 snapshot() 加锁读取，避免轮询入口白等 scanReadyTimeout。
// 未实现 AuthProber 或 toolID 未知时返回 AuthUnknown 与 nil 错误——前端轮询
// 逻辑据此继续等待，不当作异常打断弹窗流程。
func (a *App) CheckToolAuth(toolID string) (string, error) {
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
	if _, err := providers.ParseProvidersYAML([]byte(content)); err != nil {
		return fmt.Errorf("err.settings.yaml_parse_failed|%w", err)
	}
	return a.saveProvidersContent(content)
}

// saveProvidersContent 把已通过校验的 yaml 内容原子落盘并热重载 provider 表。
// SaveProvidersYAML 与导入路径共用。
func (a *App) saveProvidersContent(content string) error {
	path := a.snapshot().ProvidersPath
	if path == "" {
		return errNoProvidersPath
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

// ProvidersImportResult 是导入汇总：新增/覆盖/跳过的条目数。
type ProvidersImportResult struct {
	Added   int `json:"Added"`
	Updated int `json:"Updated"`
	Skipped int `json:"Skipped"`
}

// ImportProvidersFile 导入用户填好的接入模板：解析 → 与现有定义合并
// （同 id 覆盖更新、新 id 追加）→ 落盘 → 热重载。任何一步失败都不动现有配置。
func (a *App) ImportProvidersFile(path string) (ProvidersImportResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ProvidersImportResult{}, errors.New("err.settings.import_read_failed|")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ProvidersImportResult{}, fmt.Errorf("err.settings.import_read_failed|%w", err)
	}
	incoming, err := providers.ParseProvidersYAML(data)
	if err != nil {
		return ProvidersImportResult{}, fmt.Errorf("err.settings.yaml_parse_failed|%w", err)
	}

	// 现有定义按盘上文件解析；文件缺失按空列表。现有文件若损坏，按空处理
	// 会让导入静默吞掉旧定义，直接报错让用户先修。
	o := a.snapshot()
	if o.ProvidersPath == "" {
		return ProvidersImportResult{}, errNoProvidersPath
	}
	// 现有定义按盘上文件解析；文件缺失按空列表。非「不存在」的读失败（权限等）
	// 不能按空处理——那会让合并基线变成空列表，落盘覆盖丢失旧定义。
	var existing []providers.GenericSpec
	if cur, err := os.ReadFile(o.ProvidersPath); err == nil {
		if existing, err = providers.ParseProvidersYAML(cur); err != nil {
			return ProvidersImportResult{}, fmt.Errorf("err.settings.yaml_parse_failed|%w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProvidersImportResult{}, fmt.Errorf("err.settings.import_read_failed|%w", err)
	}

	merged, added, updated, skipped := providers.MergeGenericSpecs(existing, incoming)
	if added == 0 && updated == 0 {
		return ProvidersImportResult{}, errors.New("err.settings.import_empty")
	}
	content, err := providers.FormatProvidersYAML(merged)
	if err != nil {
		return ProvidersImportResult{}, err
	}
	if err := a.saveProvidersContent(content); err != nil {
		return ProvidersImportResult{}, err
	}
	return ProvidersImportResult{Added: added, Updated: updated, Skipped: skipped}, nil
}

// ExportProvidersTemplate 弹原生「另存为」把标准接入模板写到用户选的位置。
// title 由前端传（走 i18n）；用户取消时返回空串且不报错。
func (a *App) ExportProvidersTemplate(title string) (string, error) {
	target, err := pickSaveFile(a.dialogContext(), title, "kshell-providers-template.yml")
	if err != nil {
		return "", err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil // 用户取消
	}
	if err := os.WriteFile(target, []byte(providers.DefaultProvidersYAML()), 0o600); err != nil {
		return "", fmt.Errorf("err.settings.template_write_failed|%w", err)
	}
	return target, nil
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
	ASRModel         string   `json:"ASRModel"`
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
	ASRModel         string   `json:"ASRModel"`
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
			ASRModel:         p.ASRModel,
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
	prev := a.snapshot().Config.Model
	old := make(map[string]config.ModelPlatform, len(prev.Platforms))
	for _, p := range prev.Platforms {
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
			return fmt.Errorf("err.settings.model_platform_id_duplicated|%s", id)
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
			ASRModel:         strings.TrimSpace(pi.ASRModel),
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

	err := a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.Platforms = platforms
		c.Model.DefaultPlatform = def
		c.Model.ActiveModel = strings.TrimSpace(in.ActiveModel)
		// 默认模型必须属于默认平台，否则清空：悬空模型名会拿着新平台端点
		// 注入一个不存在的模型（删默认平台 / 换默认平台后的典型悬空场景）。
		// 默认平台未配模型列表时不校验（保留旧版自由输入模型名的用法）。
		if c.Model.ActiveModel != "" && def != "" {
			found := false
			hasList := false
			for _, p := range platforms {
				if p.ID != def {
					continue
				}
				hasList = len(p.Models) > 0
				for _, m := range p.Models {
					if m == c.Model.ActiveModel {
						found = true
						break
					}
				}
				break
			}
			if hasList && !found {
				c.Model.ActiveModel = ""
			}
		}
		if len(platforms) == 0 {
			// 空平台列表下 ActiveModel 无意义：留着会让配置继续被视为
			// 「kshell 托管过模型」，之后每次启动同步都清掉 ccswitch 配置。
			c.Model.ActiveModel = ""
		}
		// 保存时清空全部 legacy 单平台字段，避免 Load 再迁移。
		c.Model.Preset = ""
		c.Model.OpenAIBaseURL = ""
		c.Model.AnthropicBaseURL = ""
		c.Model.BaseURL = ""
		c.Model.APIKey = ""
		c.Model.Models = nil
		c.Model.Agents = nil
	})
	if err != nil {
		return err
	}
	// 持久同步到 OpenCode/CodeBuddy 自有配置；失败不回滚本次保存。
	a.syncAgentModelsAfterSave(prev)
	return nil
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

// modelSyncTargets 是支持平台模型同步注入的工具（agentsync.DefaultTargets 的 ID）。
var modelSyncTargets = map[string]bool{"opencode": true, "codebuddy": true, "claude": true}

// GetModelSyncOffTools 返回用户关闭了平台模型同步的工具 ID 列表（副本）。
func (a *App) GetModelSyncOffTools() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.opts.Config.Model.SyncOffTools))
	copy(out, a.opts.Config.Model.SyncOffTools)
	return out
}

// SetToolModelSync 设置单个工具是否同步平台模型，立即生效：
// 开启 → 该工具立即按当前平台同步；关闭 → 立即清除该工具里 kshell 注入的
// 配置（只清不建，与全局禁用的清理同语义），避免悬空注入物指向已删端点。
func (a *App) SetToolModelSync(toolID string, enabled bool) error {
	toolID = strings.TrimSpace(toolID)
	if !modelSyncTargets[toolID] {
		return fmt.Errorf("err.settings.model_sync_unknown_tool|%s", toolID)
	}
	err := a.saveConfig(func(c *config.Config) {
		// 必须新建 slice：saveConfig 的 cfg 是浅拷贝，原地 [:0] 复用会写穿
		// 内存配置的共享底层数组（Save 失败时内存被改坏 + 并发读数据竞争）。
		out := make([]string, 0, len(c.Model.SyncOffTools)+1)
		for _, id := range c.Model.SyncOffTools {
			if id != toolID {
				out = append(out, id)
			}
		}
		if !enabled {
			out = append(out, toolID)
		}
		c.Model.SyncOffTools = out
	})
	if err != nil {
		return err
	}
	if enabled {
		a.syncAgentModelTool(toolID)
		return nil
	}
	// 清理：保存后 SyncOffTools 已含该工具，InputFromModel → cleanupFor 走只清不建。
	// 失败 log-only：主诉求已持久化，注入物由下次启动 syncAgentModels 兜底补清。
	o := a.snapshot()
	if err := agentsync.SyncTool(o.Home, toolID, agentsync.InputFromModel(o.Config.Model)); err != nil {
		log.Printf("agentsync: %s 清理同步失败: %v", toolID, err)
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
