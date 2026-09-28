package desktop

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

var errNoProvidersPath = errors.New("providers.yaml 路径不可用（未初始化完成）")

// GetTools 返回最近一次扫描的工具安装状态（含未安装项，前端灰显）。
func (a *App) GetTools() []discovery.Tool {
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
