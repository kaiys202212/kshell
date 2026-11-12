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
	ASRModel         string   `json:"ASRModel"`
}

// ModelConfigView 是回传前端的模型配置视图。
type ModelConfigView struct {
	Enabled          bool                `json:"Enabled"`
	Platforms        []ModelPlatformView `json:"Platforms"`
	ActivePlatformID string              `json:"ActivePlatformID"`
	ActiveModel      string              `json:"ActiveModel"`
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
	Enabled          bool                 `json:"Enabled"`
	Platforms        []ModelPlatformInput `json:"Platforms"`
	ActivePlatformID string               `json:"ActivePlatformID"`
	ActiveModel      string               `json:"ActiveModel"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	views := make([]ModelPlatformView, 0, len(m.Platforms))
	for _, p := range m.Platforms {
		models := p.Models
		if models == nil {
			models = []string{}
		}
		views = append(views, ModelPlatformView{
			ID:               p.ID,
			Name:             p.Name,
			Preset:           p.Preset,
			OpenAIBaseURL:    p.OpenAIBaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			Models:           models,
			APIKeySet:        p.APIKey != "",
			ASRModel:         p.ASRModel,
		})
	}
	return ModelConfigView{
		Enabled:          m.Enabled,
		Platforms:        views,
		ActivePlatformID: m.ActivePlatformID,
		ActiveModel:      m.ActiveModel,
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

	// ActivePlatformID 必须命中某个平台，否则回落第一个；无平台则置空。
	activeID := strings.TrimSpace(in.ActivePlatformID)
	if activeID != "" && !seen[activeID] {
		activeID = ""
	}
	if activeID == "" && len(platforms) > 0 {
		activeID = platforms[0].ID
	}

	err := a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.Platforms = platforms
		c.Model.ActivePlatformID = activeID
		// 平台列表整体替换后，指向已删平台的工具选择一并清理（运行期回落第一个）。
		// 统一读写 ToolPlatforms 映射，旧单字段清空，避免真实保存路径把旧键写回盘。
		c.Model.ToolSync.ToolPlatforms = pruneToolPlatforms(platforms, c.Model.ToolSync)
		c.Model.ToolSync.ClaudePlatform = ""
		c.Model.ToolSync.CodeBuddyPlatform = ""
		c.Model.ActiveModel = strings.TrimSpace(in.ActiveModel)
		// 当前模型必须属于当前平台，否则清空：悬空模型名会拿着新平台端点
		// 调用一个不存在的模型（删当前平台 / 换当前平台后的典型悬空场景）。
		// 当前平台未配模型列表时不校验（保留旧版自由输入模型名的用法）。
		if c.Model.ActiveModel != "" && activeID != "" {
			found := false
			hasList := false
			for _, p := range platforms {
				if p.ID != activeID {
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

// validPlatformID 返回命中平台列表的 ID；空或悬空返回空串（运行期回落第一个）。
func validPlatformID(platforms []config.ModelPlatform, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, p := range platforms {
		if p.ID == id {
			return id
		}
	}
	return ""
}

// toolPlatformsSnapshot 返回工具平台映射副本（含旧字段兜底）：旧
// claude_platform/codebuddy_platform 只在映射缺少对应键时补入。必须新建 map，
// saveConfig 的浅拷贝下原地改写会写穿内存配置。
func toolPlatformsSnapshot(ts config.ToolSyncConfig) map[string]string {
	out := make(map[string]string, len(ts.ToolPlatforms)+2)
	for tool, id := range ts.ToolPlatforms {
		tool = strings.TrimSpace(tool)
		if id = strings.TrimSpace(id); tool != "" && id != "" {
			out[tool] = id
		}
	}
	for _, tool := range []string{"claude", "codebuddy"} {
		if _, ok := out[tool]; ok {
			continue
		}
		if id := ts.ToolPlatformID(tool); id != "" {
			out[tool] = id
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pruneToolPlatforms 返回剔除悬空平台后的映射副本：平台列表整体替换后，
// 指向已删平台的选择置空（运行期回落第一个）。
func pruneToolPlatforms(platforms []config.ModelPlatform, ts config.ToolSyncConfig) map[string]string {
	out := toolPlatformsSnapshot(ts)
	for tool, id := range out {
		if valid := validPlatformID(platforms, id); valid == "" {
			delete(out, tool)
		} else {
			out[tool] = valid
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// removeToolPlatforms 返回剔除指定平台的映射副本（删除平台/多平台编辑共用）。
func removeToolPlatforms(ts config.ToolSyncConfig, platformID string) map[string]string {
	out := toolPlatformsSnapshot(ts)
	for tool, id := range out {
		if strings.TrimSpace(id) == platformID {
			delete(out, tool)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// setToolPlatform 写入单个工具平台；空 ID 表示删除该键（回落第一个）。
func setToolPlatform(m map[string]string, toolID, id string) {
	if id = strings.TrimSpace(id); id == "" {
		delete(m, toolID)
		return
	}
	m[toolID] = id
}

// ClaudeSlotsView / ToolSyncView / ToolSyncInput 是按工具定制模型同步的绑定视图：
// Claude 为 4 类映射槽位，OpenCode 为平台 ID → 勾选模型，CodeBuddy 为所选平台勾选模型。
type ClaudeSlotsView struct {
	Model  string `json:"Model"`
	Sonnet string `json:"Sonnet"`
	Opus   string `json:"Opus"`
	Haiku  string `json:"Haiku"`
}

type ToolSyncView struct {
	Enabled           bool                `json:"Enabled"`
	Claude            ClaudeSlotsView     `json:"Claude"`
	ClaudePlatform    string              `json:"ClaudePlatform"`
	OpenCode          map[string][]string `json:"OpenCode"`
	CodeBuddy         []string            `json:"CodeBuddy"`
	CodeBuddyPlatform string              `json:"CodeBuddyPlatform"`
}

type ToolSyncInput struct {
	Enabled           bool                `json:"Enabled"`
	Claude            ClaudeSlotsView     `json:"Claude"`
	ClaudePlatform    string              `json:"ClaudePlatform"`
	OpenCode          map[string][]string `json:"OpenCode"`
	CodeBuddy         []string            `json:"CodeBuddy"`
	CodeBuddyPlatform string              `json:"CodeBuddyPlatform"`
}

// SetActiveModelSelection 成对设置 kshell 当前平台与默认模型并立即落盘生效，
// 随后调度一次防抖同步。平台不存在时回落第一个；模型不在生效平台模型列表时
// 清空（列表为空保留自由输入），无平台则两者一并清空。
func (a *App) SetActiveModelSelection(platformID, model string) error {
	platformID = strings.TrimSpace(platformID)
	model = strings.TrimSpace(model)
	err := a.saveConfig(func(c *config.Config) {
		found := false
		for _, p := range c.Model.Platforms {
			if p.ID == platformID {
				found = true
				break
			}
		}
		if !found {
			platformID = ""
			if len(c.Model.Platforms) > 0 {
				platformID = c.Model.Platforms[0].ID
			}
		}
		if platformID == "" {
			model = ""
		} else {
			for _, p := range c.Model.Platforms {
				if p.ID != platformID {
					continue
				}
				if len(p.Models) > 0 {
					hit := false
					for _, m := range p.Models {
						if m == model {
							hit = true
							break
						}
					}
					if !hit {
						model = ""
					}
				}
				break
			}
		}
		c.Model.ActivePlatformID = platformID
		c.Model.ActiveModel = model
	})
	if err != nil {
		return err
	}
	a.scheduleAgentSync()
	return nil
}

// SetModelEnabled 设置模型平台总开关并立即落盘生效，随后调度一次防抖同步：
// 关闭时 SyncInput.Enabled=false，各目标走既有清理分支清掉 kshell 注入物。
func (a *App) SetModelEnabled(enabled bool) error {
	if err := a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = enabled
	}); err != nil {
		return err
	}
	a.scheduleAgentSync()
	return nil
}

// DeleteModelPlatform 删除单个平台并立即落盘生效，随后调度一次防抖同步。
// 删除的是当前平台时回落第一个；新当前平台有模型列表且不含 ActiveModel 时
// 悬空清空；指向被删平台的工具平台一并置空（运行期回落第一个）。
func (a *App) DeleteModelPlatform(platformID string) error {
	platformID = strings.TrimSpace(platformID)
	exists := false
	for _, p := range a.snapshot().Config.Model.Platforms {
		if p.ID == platformID {
			exists = true
			break
		}
	}
	if !exists {
		return fmt.Errorf("err.settings.model_platform_not_found|%s", platformID)
	}
	err := a.saveConfig(func(c *config.Config) {
		rest := make([]config.ModelPlatform, 0, len(c.Model.Platforms))
		for _, p := range c.Model.Platforms {
			if p.ID == platformID {
				continue
			}
			rest = append(rest, p)
		}
		c.Model.Platforms = rest
		if c.Model.ActivePlatformID == platformID {
			if len(rest) > 0 {
				c.Model.ActivePlatformID = rest[0].ID
			} else {
				c.Model.ActivePlatformID = ""
			}
		}
		c.Model.ToolSync.ToolPlatforms = removeToolPlatforms(c.Model.ToolSync, platformID)
		c.Model.ToolSync.ClaudePlatform = ""
		c.Model.ToolSync.CodeBuddyPlatform = ""
		if c.Model.ActiveModel != "" {
			def, _ := c.Model.ActivePlatform()
			if def.ID == "" {
				c.Model.ActiveModel = ""
			} else if len(def.Models) > 0 {
				hit := false
				for _, m := range def.Models {
					if m == c.Model.ActiveModel {
						hit = true
						break
					}
				}
				if !hit {
					c.Model.ActiveModel = ""
				}
			}
		}
	})
	if err != nil {
		return err
	}
	a.scheduleAgentSync()
	return nil
}

// GetToolSyncConfig 返回单个工具的定制模型同步配置（视图副本）。
func (a *App) GetToolSyncConfig(toolID string) (ToolSyncView, error) {
	toolID = strings.TrimSpace(toolID)
	if !modelSyncTargets[toolID] {
		return ToolSyncView{}, fmt.Errorf("err.settings.model_sync_unknown_tool|%s", toolID)
	}
	m := a.snapshot().Config.Model
	ts := m.ToolSync
	var openCode map[string][]string
	if len(ts.OpenCode) > 0 {
		openCode = make(map[string][]string, len(ts.OpenCode))
		for k, v := range ts.OpenCode {
			openCode[k] = append([]string(nil), v...)
		}
	}
	return ToolSyncView{
		Enabled:           ts.Enabled[toolID],
		Claude:            ClaudeSlotsView{Model: ts.Claude.Model, Sonnet: ts.Claude.Sonnet, Opus: ts.Claude.Opus, Haiku: ts.Claude.Haiku},
		ClaudePlatform:    ts.ToolPlatformID("claude"),
		OpenCode:          openCode,
		CodeBuddy:         append([]string(nil), ts.CodeBuddy...),
		CodeBuddyPlatform: ts.ToolPlatformID("codebuddy"),
	}, nil
}

// SetToolSyncConfig 持久化单个工具的定制模型同步配置并立即对该工具生效：
// 启用 → 按勾选注入；禁用 → 只清不建（勾选一并清空，避免残留悬空配置）。
// 各工具只改写自己的配置段；失败 log-only（主诉求已持久化，下次启动兜底）。
func (a *App) SetToolSyncConfig(toolID string, in ToolSyncInput) error {
	toolID = strings.TrimSpace(toolID)
	if !modelSyncTargets[toolID] {
		return fmt.Errorf("err.settings.model_sync_unknown_tool|%s", toolID)
	}
	trimList := func(list []string) []string {
		out := make([]string, 0, len(list))
		for _, v := range list {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	err := a.saveConfig(func(c *config.Config) {
		// saveConfig 的 cfg 是浅拷贝：map/slice 必须新建，原地复用会写穿共享底层数组。
		// 平台选择统一读写 ToolPlatforms；旧字段只作兜底迁入、不再写回盘。
		toolPlatforms := toolPlatformsSnapshot(c.Model.ToolSync)
		if toolPlatforms == nil {
			toolPlatforms = map[string]string{}
		}
		ts := config.ToolSyncConfig{
			Enabled:       map[string]bool{},
			Claude:        c.Model.ToolSync.Claude,
			ToolPlatforms: toolPlatforms,
			OpenCode:      map[string][]string{},
			CodeBuddy:     append([]string(nil), c.Model.ToolSync.CodeBuddy...),
		}
		for k, v := range c.Model.ToolSync.Enabled {
			ts.Enabled[k] = v
		}
		for k, v := range c.Model.ToolSync.OpenCode {
			ts.OpenCode[k] = append([]string(nil), v...)
		}
		switch toolID {
		case "claude":
			if in.Enabled {
				ts.Claude = config.ClaudeSync{
					Model:  strings.TrimSpace(in.Claude.Model),
					Sonnet: strings.TrimSpace(in.Claude.Sonnet),
					Opus:   strings.TrimSpace(in.Claude.Opus),
					Haiku:  strings.TrimSpace(in.Claude.Haiku),
				}
				setToolPlatform(ts.ToolPlatforms, "claude", validPlatformID(c.Model.Platforms, in.ClaudePlatform))
			} else {
				ts.Claude = config.ClaudeSync{}
				delete(ts.ToolPlatforms, "claude")
			}
		case "opencode":
			// OpenCode 是 tool_sync.opencode 键的唯一属主：禁用即整键清空。
			ts.OpenCode = map[string][]string{}
			if in.Enabled {
				for k, v := range in.OpenCode {
					if list := trimList(v); len(list) > 0 {
						ts.OpenCode[k] = list
					}
				}
			}
			if len(ts.OpenCode) == 0 {
				ts.OpenCode = nil
			}
		case "codebuddy":
			if in.Enabled {
				ts.CodeBuddy = trimList(in.CodeBuddy)
				setToolPlatform(ts.ToolPlatforms, "codebuddy", validPlatformID(c.Model.Platforms, in.CodeBuddyPlatform))
			} else {
				ts.CodeBuddy = nil
				delete(ts.ToolPlatforms, "codebuddy")
			}
		}
		if len(ts.ToolPlatforms) == 0 {
			ts.ToolPlatforms = nil
		}
		if in.Enabled {
			ts.Enabled[toolID] = true
		} else {
			delete(ts.Enabled, toolID)
		}
		c.Model.ToolSync = ts
	})
	if err != nil {
		return err
	}
	a.syncAgentModelTool(toolID)
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

// AgentHubSettingsView 是 AgentHub 设置的绑定读取形态（Enabled 已解析为 bool）。
type AgentHubSettingsView struct {
	Enabled          bool     `json:"enabled"`
	HeartbeatMinutes int      `json:"heartbeat_minutes"`
	DirectPatterns   []string `json:"direct_patterns"`
	DefaultTool      string   `json:"default_tool"`
	DefaultModel     string   `json:"default_model"`
	RouteMode        string   `json:"route_mode"`
}

// AgentHubSettingsInput 是可编辑字段的保存形态；Enabled 走 SetAgentHubEnabled
// 独立翻转（开关即时生效），DefaultTool/DefaultModel/RouteMode 由既有写穿绑定负责。
type AgentHubSettingsInput struct {
	HeartbeatMinutes int      `json:"heartbeat_minutes"`
	DirectPatterns   []string `json:"direct_patterns"`
}

// GetAgentHubSettings 返回 AgentHub 设置快照。
func (a *App) GetAgentHubSettings() AgentHubSettingsView {
	ah := a.snapshot().Config.AgentHub
	return AgentHubSettingsView{
		Enabled:          ah.EnabledOn(),
		HeartbeatMinutes: ah.HeartbeatMinutes,
		DirectPatterns:   ah.DirectPatterns,
		DefaultTool:      ah.DefaultTool,
		DefaultModel:     ah.DefaultModel,
		RouteMode:        ah.RouteMode,
	}
}

// SaveAgentHubSettings 保存编排参数并即时生效（微信桥门控重读配置）。
func (a *App) SaveAgentHubSettings(in AgentHubSettingsInput) error {
	if err := a.saveConfig(func(c *config.Config) {
		c.AgentHub.HeartbeatMinutes = in.HeartbeatMinutes
		c.AgentHub.DirectPatterns = in.DirectPatterns
		c.AgentHub = c.AgentHub.Normalized()
	}); err != nil {
		return err
	}
	a.applyAgentHubGates()
	return nil
}

// SetAgentHubEnabled 翻转总开关并即时生效。
func (a *App) SetAgentHubEnabled(enabled bool) error {
	if err := a.saveConfig(func(c *config.Config) {
		c.AgentHub.Enabled = &enabled
	}); err != nil {
		return err
	}
	a.applyAgentHubGates()
	return nil
}

// applyAgentHubGates 让总开关/白名单改动即时生效：微信桥按最新配置重装配
// （微信桥按最新配置重建实例：startWeixin 替换前 Stop 旧实例，关闭总开关时旧长轮询随之停止；开启时恢复）。
func (a *App) applyAgentHubGates() {
	a.startWeixin()
}
