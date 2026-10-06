package config

import (
	"os"
	"path/filepath"
)

const rootDirName = ".kshell"

// Layout 描述 ~/.kshell 下的全部持久化位置，集中一处便于测试与后续扩展。
type Layout struct {
	Root        string
	Config      string
	Connections string
	Providers   string
	Cache       string
	CacheIndex  string
	// CacheSnapshot 是上次扫描结果的快照：启动时先端出它让界面秒开，后台再扫描刷新。
	CacheSnapshot string
	// CacheTools 是工具版本探测结果的缓存：可执行文件未变就跳过 `<bin> --version` 子进程。
	CacheTools string
	// SignalExit 是退出信号文件：外部脚本（如 build.ps1）创建它即可请求
	// 运行中的桌面版优雅退出，供无人值守构建/调试使用。
	SignalExit string
	// Projects 是项目表：手动添加的项目目录 + 逻辑删除（隐藏）的项目。
	Projects string
	// Archived 是已归档会话 ID 名单。
	Archived string
	// State 是自动修复/卸载标记的落盘目录（~/.kshell/state）：卸载标记阻止
	// 残缺自动修复把刚卸的装回来，repair 计数限制自动修复重试次数。
	State string
	// CacheAppearance 是颜色主题注入文件的落盘目录（agent 工具用）。
	CacheAppearance string
}

func Paths() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	root := filepath.Join(home, rootDirName)
	cache := filepath.Join(root, "cache")

	return Layout{
		Root:            root,
		Config:          filepath.Join(root, "config.yaml"),
		Connections:     filepath.Join(root, "connections.yaml"),
		Providers:       filepath.Join(root, "providers.yaml"),
		Cache:           cache,
		CacheIndex:      filepath.Join(cache, "index.json"),
		CacheSnapshot:   filepath.Join(cache, "snapshot.json"),
		CacheTools:      filepath.Join(cache, "tools.json"),
		SignalExit:      filepath.Join(root, "exit.signal"),
		Projects:        filepath.Join(root, "projects.yaml"),
		Archived:        filepath.Join(root, "archived.json"),
		State:           filepath.Join(root, "state"),
		CacheAppearance: filepath.Join(cache, "appearance"),
	}, nil
}

func EnsureRoot(p Layout) error {
	return os.MkdirAll(p.Cache, 0o755)
}
