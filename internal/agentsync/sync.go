package agentsync

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/config"
)

// SyncInput 是写入各工具自有配置的快照（来自 kshell 模型平台）。
type SyncInput struct {
	Enabled   bool
	Platforms []config.ModelPlatform
	// DefaultPlatformID 是静态注入类目标（如 Claude settings.json 只有一份
	// env）需要落盘的平台；空 = 第一个平台。
	DefaultPlatformID string
}

// Target 是某一个 agent 的自有配置同步适配器。
type Target interface {
	ToolID() string
	// ConfigExists 报告用户目录下是否已有该工具配置（曾同步或用户/其它工具写过）。
	ConfigExists(home string) bool
	Sync(home string, in SyncInput) error
}

// DefaultTargets 返回同步目标：OpenCode + CodeBuddy + Claude。
func DefaultTargets() []Target {
	return []Target{OpenCode{}, CodeBuddy{}, Claude{}}
}

// SyncAll 对已安装（或已有配置文件）的目标做命名空间同步。
// 单目标失败不中断其余；返回第一个错误（供日志）。
func SyncAll(home string, in SyncInput, installedIDs []string) error {
	installed := map[string]bool{}
	for _, id := range installedIDs {
		installed[strings.TrimSpace(id)] = true
	}
	var first error
	for _, t := range DefaultTargets() {
		if !installed[t.ToolID()] && !t.ConfigExists(home) {
			continue
		}
		if err := t.Sync(home, in); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SyncTool 同步单个工具（安装成功路径）；总是尝试写入（可创建配置目录）。
func SyncTool(home, toolID string, in SyncInput) error {
	toolID = strings.TrimSpace(toolID)
	for _, t := range DefaultTargets() {
		if t.ToolID() == toolID {
			return t.Sync(home, in)
		}
	}
	return nil
}

// InputFromModel 从 config.ModelConfig 组装 SyncInput。
func InputFromModel(m config.ModelConfig) SyncInput {
	return SyncInput{Enabled: m.Enabled, Platforms: m.Platforms, DefaultPlatformID: m.DefaultPlatform}
}

// writeAtomic 先写临时文件再 rename，避免半截 JSON。
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// providerKey 返回 OpenCode provider 键：kshell-<platformID>。
func providerKey(platformID string) string {
	return "kshell-" + platformID
}

// isKshellProviderKey 报告是否为 kshell 管理的 OpenCode provider 键。
func isKshellProviderKey(key string) bool {
	return strings.HasPrefix(key, "kshell-")
}
