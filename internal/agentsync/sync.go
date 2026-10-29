package agentsync

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/config"
)

// renameAttempts 是 writeAtomic 遇到 Windows sharing violation 时的重试次数；
// 退避为 1ms、2ms……累计约 15ms，足以覆盖并发写者的重命名窗口。
const renameAttempts = 6

// SyncInput 是写入各工具自有配置的快照（来自 kshell 模型平台）。
type SyncInput struct {
	Enabled   bool
	Platforms []config.ModelPlatform
	// DefaultPlatformID 是静态注入类目标（如 Claude settings.json 只有一份
	// env）需要落盘的平台；空 = 第一个平台。
	DefaultPlatformID string
	// ActiveModel 是 Claude 静态注入的 ANTHROPIC_MODEL；空则回落默认平台
	// 模型列表首项。终端 Claude TUI 的 /model 依赖 settings.json 的这些键。
	ActiveModel string
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
	return SyncInput{
		Enabled:           m.Enabled,
		Platforms:         m.Platforms,
		DefaultPlatformID: m.DefaultPlatform,
		ActiveModel:       m.ActiveModel,
	}
}

// writeAtomic 先写临时文件再 rename，避免半截 JSON。
// 临时名必须唯一：TUI 与桌面端可能同时写同一目标，固定 "<path>.tmp" 会互相截断
// 或抢先 rename 掉对方的临时文件。
// rename 冲突要重试：Windows 上另一写者/读者短暂持有目标文件时 Rename 会报
// sharing violation（实测 2 个并发写者约 16% 失败），退避几次即可稳定。
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	for attempt := 0; ; attempt++ {
		err = os.Rename(tmp, path)
		if err == nil || attempt >= renameAttempts-1 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// providerKey 返回 OpenCode provider 键：kshell-<platformID>。
func providerKey(platformID string) string {
	return "kshell-" + platformID
}

// isKshellProviderKey 报告是否为 kshell 管理的 OpenCode provider 键。
func isKshellProviderKey(key string) bool {
	return strings.HasPrefix(key, "kshell-")
}
