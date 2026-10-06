// Package launch 把「会话/工作区 + 工具表」解析成可启动的命令描述，
// TUI（internal/ui）与桌面版（internal/desktop）共用，避免两边各写一份查找逻辑。
package launch

import (
	"errors"
	"os"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/cursoragent"
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

// ModelOptions 描述模型注入：Resolver 按 toolID 返回该工具应注入的配置（nil 表示不注入）。
type ModelOptions struct {
	Resolver func(toolID string) (providers.ModelConfig, bool)
}

// PermissionOptions 描述权限注入：Bypass 为真时按工具追加跳过确认参数。
type PermissionOptions struct {
	Bypass bool
}

// applyModel 与 applyTheme 同构：provider 实现 ModelInjector 时注入环境变量，
// 并把模型参数**前置**到已有参数之前（全局参数须在子命令如 codex resume 之前）。
func applyModel(l providers.Launch, p providers.Provider, mo ModelOptions) providers.Launch {
	if mo.Resolver == nil {
		return l
	}
	cfg, ok := mo.Resolver(p.ID())
	if !ok {
		return l
	}
	if inj, ok := p.(providers.ModelInjector); ok {
		args, env := inj.InjectModel(cfg)
		if len(args) > 0 {
			// 前置：模型参数作为全局参数放在子命令（如 codex resume）之前，避免被 clap 拒绝
			l.Args = append(append([]string(nil), args...), l.Args...)
		}
		l.Env = providers.MergeEnv(l.Env, env)
	}
	return l
}

// applyPermission 前置权限相关全局参数（与模型参数同样须在子命令之前）。
func applyPermission(l providers.Launch, p providers.Provider, po PermissionOptions) providers.Launch {
	if !po.Bypass {
		return l
	}
	if inj, ok := p.(providers.PermissionInjector); ok {
		args, env := inj.InjectPermission(true)
		if len(args) > 0 {
			l.Args = append(append([]string(nil), args...), l.Args...)
		}
		l.Env = providers.MergeEnv(l.Env, env)
	}
	return l
}

func finalize(l providers.Launch, p providers.Provider, mo ModelOptions, po PermissionOptions) providers.Launch {
	return applyPermission(applyModel(l, p, mo), p, po)
}

// prependBinArgs 把入口前缀参数（node 入口形态下为主脚本名）放到最前，使命令成为
// `node.exe index.js [注入参数 + provider 参数...]`。必须最后执行：注入参数若落在
// 脚本名之前，node 会把它当自身选项直接拒绝（`node --force` → bad option）。
func prependBinArgs(l providers.Launch, tool discovery.Tool) providers.Launch {
	if len(tool.BinArgs) == 0 {
		return l
	}
	l.Args = append(append([]string(nil), tool.BinArgs...), l.Args...)
	return l
}

// compose 组装启动描述：主题 → 模型/权限注入（前置到 provider 参数）→ 最后入口前缀。
// 无 BinArgs 的工具（非 node 入口）行为等价于直接 finalize。
func compose(l providers.Launch, p providers.Provider, tool discovery.Tool, to ThemeOptions, mo ModelOptions, po PermissionOptions) providers.Launch {
	return prependBinArgs(finalize(applyTheme(l, p, to), p, mo, po), tool)
}

// ForSession 根据会话找到对应 provider 与可执行文件，产出恢复会话的启动描述。
// tools 为 discovery.DetectAll 的产物；会话所属工具不在表内或不可执行时报 ErrToolNotRunnable。
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session, to ThemeOptions, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return compose(p.ResumeCmd(s, tool.BinPath), p, tool, to, mo, po), nil
}

// ForWorkspace 为工作区挑选首选工具（优先该工作区会话数最多的），产出新建会话的启动描述。
func ForWorkspace(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, to ThemeOptions, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	return ForWorkspaceTool(ps, tools, ws, "", to, mo, po)
}

// ForWorkspaceTool 用指定工具产出新建会话的启动描述（前端「新建会话可选工具」用）。
// toolID 为空时回退到 ForWorkspace 的首选逻辑；
// 指定的工具未安装（或没有可执行文件、没有对应 provider）时报 ErrToolNotRunnable。
func ForWorkspaceTool(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, to ThemeOptions, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	if toolID == "" {
		p, tool, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		return compose(p.NewSessionCmd(ws.Path, tool.BinPath), p, tool, to, mo, po), nil
	}

	tool, ok := toolFor(tools, toolID)
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, toolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return compose(p.NewSessionCmd(ws.Path, tool.BinPath), p, tool, to, mo, po), nil
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
func ForSessionACP(ps []providers.Provider, tools []discovery.Tool, s providers.Session, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	tool, ok := toolFor(tools, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	l, err := acpLaunch(tool, s.Workspace)
	if err != nil {
		return providers.Launch{}, err
	}
	return finalize(l, p, mo, po), nil
}

// ForWorkspaceACP 产出在工作区新建 ACP 会话的启动描述；toolID 为空时用首选工具。
func ForWorkspaceACP(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	var tool discovery.Tool
	var p providers.Provider
	if toolID == "" {
		pp, t, ok := PreferredTool(ps, tools, ws)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		p, tool = pp, t
	} else {
		t, ok := toolFor(tools, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		pp, ok := providerFor(ps, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool, p = t, pp
	}
	l, err := acpLaunch(tool, ws.Path)
	if err != nil {
		return providers.Launch{}, err
	}
	return finalize(l, p, mo, po), nil
}

func acpLaunch(tool discovery.Tool, dir string) (providers.Launch, error) {
	if tool.ACP == nil || !tool.ACP.Available {
		return providers.Launch{}, ErrACPUnavailable
	}
	l := providers.Launch{Dir: dir}
	if tool.ACP.Source == "npx" {
		path := tool.ACP.BinPath
		if path == "" {
			path = "npx" // 兜底：探测结果缺 BinPath 时用 PATH 上的 npx
		}
		l.Path, l.Args = path, append([]string{"-y", tool.ACP.Package}, tool.ACP.ExtraArgs...)
	} else {
		l.Path, l.Args = tool.ACP.BinPath, append([]string(nil), tool.ACP.ExtraArgs...)
	}
	l.Env = acpAdapterEnv(tool)
	return l, nil
}

// acpAdapterEnv 为依赖本体 CLI 的适配器补充环境变量。目前只有 cursor-acp：
// cursor-agent 常不在 PATH 上（Windows 装在 %LOCALAPPDATA%\cursor-agent），
// 适配器默认只按 PATH 查找，须显式传 CURSOR_AGENT_EXECUTABLE。
// node 入口形态无法被 Node spawn 成单一文件（.cmd 会 EINVAL），改把当前 kshell
// 可执行文件当代理，由代理再 exec node + index.js。
func acpAdapterEnv(tool discovery.Tool) map[string]string {
	if tool.ID != "cursor" || tool.BinPath == "" {
		return nil
	}
	if len(tool.BinArgs) == 0 {
		return map[string]string{"CURSOR_AGENT_EXECUTABLE": tool.BinPath}
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// 解析不到自身路径时无法提供可 spawn 的 PE，退回不注入（cursor-acp 仍会 ENOENT）。
		return nil
	}
	return map[string]string{
		"CURSOR_AGENT_EXECUTABLE": exe,
		cursoragent.EnvAsProxy:    "1",
		cursoragent.EnvNode:       tool.BinPath,
		cursoragent.EnvScript:     tool.BinArgs[0],
	}
}
