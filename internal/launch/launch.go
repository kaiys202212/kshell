// Package launch 把「会话/工作区 + 工具表」解析成可启动的命令描述，
// TUI（internal/ui）与桌面版（internal/desktop）共用，避免两边各写一份查找逻辑。
package launch

import (
	"errors"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// ErrToolNotRunnable 表示目标工具未安装或没有可执行文件。
var ErrToolNotRunnable = errors.New("该工具没有可执行程序，无法启动会话")

// ThemeOptions 描述主题注入所需信息：模式 + 生成文件落盘目录（空则跳过文件类注入）。
type ThemeOptions struct {
	Mode     appearance.Mode
	CacheDir string
}

// applyTheme 给启动描述注入主题：总是追加通用颜色变量，provider 实现 Themer 时再追加专属参数/变量。
func applyTheme(l providers.Launch, p providers.Provider, to ThemeOptions) providers.Launch {
	theme := appearance.Resolve(to.Mode)
	l.Env = providers.MergeEnv(l.Env, appearance.GenericEnv(theme))
	if th, ok := p.(providers.Themer); ok {
		args, env := th.ThemeOverrides(theme, to.CacheDir)
		l.Args = append(l.Args, args...)
		l.Env = providers.MergeEnv(l.Env, env)
	}
	return l
}

// ForSession 根据会话找到对应 provider 与可执行文件，产出恢复会话的启动描述。
// tools 为 discovery.DetectAll 的产物；会话所属工具不在表内或不可执行时报 ErrToolNotRunnable。
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session, to ThemeOptions) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return applyTheme(p.ResumeCmd(s, tool.BinPath), p, to), nil
}

// ForWorkspace 为工作区挑选首选工具（优先该工作区会话数最多的），产出新建会话的启动描述。
func ForWorkspace(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, to ThemeOptions) (providers.Launch, error) {
	return ForWorkspaceTool(ps, tools, ws, "", to)
}

// ForWorkspaceTool 用指定工具产出新建会话的启动描述（前端「新建会话可选工具」用）。
// toolID 为空时回退到 ForWorkspace 的首选逻辑；
// 指定的工具未安装（或没有可执行文件、没有对应 provider）时报 ErrToolNotRunnable。
func ForWorkspaceTool(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, to ThemeOptions) (providers.Launch, error) {
	if toolID == "" {
		p, tool, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		return applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), nil
	}

	tool, ok := toolFor(tools, toolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, toolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return applyTheme(p.NewSessionCmd(ws.Path, tool.BinPath), p, to), nil
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

// ErrACPUnavailable 表示目标工具没有可用的 ACP 适配器。
var ErrACPUnavailable = errors.New("该工具没有可用的 ACP 适配器")

// ForSessionACP 产出用 ACP 恢复历史会话的启动描述（命令来自适配器探测）。
func ForSessionACP(ps []providers.Provider, tools []discovery.Tool, s providers.Session) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return acpLaunch(tool, s.Workspace)
}

// ForWorkspaceACP 产出在工作区新建 ACP 会话的启动描述；toolID 为空时用首选工具。
func ForWorkspaceACP(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string) (providers.Launch, error) {
	var tool discovery.Tool
	if toolID == "" {
		_, t, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool = t
	} else {
		t, ok := toolFor(tools, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool = t
	}
	return acpLaunch(tool, ws.Path)
}

func acpLaunch(tool discovery.Tool, dir string) (providers.Launch, error) {
	if tool.ACP == nil || !tool.ACP.Available {
		return providers.Launch{}, ErrACPUnavailable
	}
	if tool.ACP.Source == "npx" {
		path := tool.ACP.BinPath
		if path == "" {
			path = "npx" // 兜底：探测结果缺 BinPath 时用 PATH 上的 npx
		}
		return providers.Launch{Path: path, Args: append([]string{"-y", tool.ACP.Package}, tool.ACP.ExtraArgs...), Dir: dir}, nil
	}
	return providers.Launch{Path: tool.ACP.BinPath, Args: append([]string(nil), tool.ACP.ExtraArgs...), Dir: dir}, nil
}
