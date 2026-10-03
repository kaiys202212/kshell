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
	a.mu.Lock()
	empty := len(a.tools) == 0
	a.mu.Unlock()
	if empty {
		a.ensureScanReady()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tools == nil {
		return []discovery.Tool{}
	}
	return a.tools
}

// SaveProvidersYAML 保存自定义工具定义：先解析校验（合法但内容为空也允许），
// 再临时文件原子替换写回，避免写一半崩溃损坏配置。
// 注意：保存后不热生效——新自定义 provider 需重启应用后由重扫装配（后续任务接线）。
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
	return os.Rename(tmpName, path)
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
	Enabled   bool              `json:"Enabled"`
	BaseURL   string            `json:"BaseURL"`
	Agents    map[string]string `json:"Agents"`
	APIKeySet bool              `json:"APIKeySet"`
}

// ModelConfigInput 是前端提交的模型配置：APIKey 空串=保持原值，ClearAPIKey 显式清除。
type ModelConfigInput struct {
	Enabled     bool              `json:"Enabled"`
	BaseURL     string            `json:"BaseURL"`
	APIKey      string            `json:"APIKey"`
	ClearAPIKey bool              `json:"ClearAPIKey"`
	Agents      map[string]string `json:"Agents"`
}

func (a *App) GetModelConfig() (ModelConfigView, error) {
	m := a.snapshot().Config.Model
	agents := m.Agents
	if agents == nil {
		agents = map[string]string{}
	}
	return ModelConfigView{
		Enabled:   m.Enabled,
		BaseURL:   m.BaseURL,
		Agents:    agents,
		APIKeySet: m.APIKey != "",
	}, nil
}

func (a *App) SetModelConfig(in ModelConfigInput) error {
	base := strings.TrimSpace(in.BaseURL)
	if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return fmt.Errorf("Base URL 必须以 http:// 或 https:// 开头")
	}
	agents := map[string]string{}
	for k, v := range in.Agents {
		agents[k] = strings.TrimSpace(v)
	}
	return a.saveConfig(func(c *config.Config) {
		c.Model.Enabled = in.Enabled
		c.Model.BaseURL = base
		c.Model.Agents = agents
		switch {
		case in.ClearAPIKey:
			c.Model.APIKey = ""
		case in.APIKey != "":
			c.Model.APIKey = in.APIKey
		}
	})
}
