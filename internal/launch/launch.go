// Package launch 把「会话/工作区 + 工具表」解析成可启动的命令描述，
// TUI（internal/ui）与桌面版（internal/desktop）共用，避免两边各写一份查找逻辑。
package launch

import (
	"errors"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// ErrToolNotRunnable 表示目标工具未安装或没有可执行文件。
var ErrToolNotRunnable = errors.New("该工具没有可执行程序，无法启动会话")

// ForSession 根据会话找到对应 provider 与可执行文件，产出恢复会话的启动描述。
// tools 为 discovery.DetectAll 的产物；会话所属工具不在表内或不可执行时报 ErrToolNotRunnable。
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return p.ResumeCmd(s, tool.BinPath), nil
}

// ForWorkspace 为工作区挑选首选工具（优先该工作区会话数最多的），产出新建会话的启动描述。
// ctxFiles 为上下文篮里的文件路径，作为初始提示注入新会话。
func ForWorkspace(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, ctxFiles []string) (providers.Launch, error) {
	p, tool, ok := PreferredTool(ps, tools, ws)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return p.NewSessionCmd(ws.Path, tool.BinPath, ctxFiles), nil
}

// PreferredTool 选一个该工作区里可用（已安装且有可执行文件）的工具；优先会话数最多的。
func PreferredTool(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace) (providers.Provider, discovery.Tool, bool) {
	bestCount := -1
	var bestProvider providers.Provider
	var bestTool discovery.Tool

	for _, p := range ps {
		tool, ok := toolFor(tools, p.ID())
		if !ok || !tool.Installed || tool.BinPath == "" {
			continue
		}
		count := ws.ToolCounts[p.ID()]
		if count > bestCount {
			bestCount, bestProvider, bestTool = count, p, tool
		}
	}
	return bestProvider, bestTool, bestProvider != nil
}

func toolFor(tools []discovery.Tool, id string) (discovery.Tool, bool) {
	for _, t := range tools {
		if t.ID == id {
			return t, true
		}
	}
	return discovery.Tool{}, false
}

func providerFor(ps []providers.Provider, id string) (providers.Provider, bool) {
	for _, p := range ps {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}
