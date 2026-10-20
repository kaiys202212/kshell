// Package launch 把「会话/工作区 + 工具表」解析成可启动的命令描述，
// TUI（internal/ui）与桌面版（internal/desktop）共用，避免两边各写一份查找逻辑。
package launch

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/claudeenv"
	"github.com/yangk/kshell/internal/cursoragent"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// ErrToolNotRunnable 表示目标工具未安装或没有可执行文件。
var ErrToolNotRunnable = errors.New("err.launch.no_binary")

// ErrToolUpdating 表示目标工具正处于官方自更新窗口（入口被更新器临时清掉）。
// 与 ErrToolNotRunnable 区分开：前端译成「正在自更新，请稍后重试」而非「无二进制」。
var ErrToolUpdating = errors.New("err.launch.updating")

// ErrNotLoggedIn 表示目标工具需要登录但当前未登录（新建 ACP 会话被门禁拒绝）。
var ErrNotLoggedIn = errors.New("err.launch.not_logged_in")

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
// 另支持 SettingsEnvInjector：模型 env 经 `--settings` 的 env 块注入——
// claude 会用用户级 settings.json 的 env 替换进程环境变量，走 env 注入必被覆盖。
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
	if se, ok := p.(providers.SettingsEnvInjector); ok {
		if env := se.SettingsEnv(cfg); len(env) > 0 {
			inner := make(map[string]any, len(env))
			for k, v := range env {
				inner[k] = v
			}
			// 合并进 l.Args 既有的 --settings（主题注入先行时已有产物）；
			// 没有则前置一组（见 MergeSettingsArg 注释）。env 键做键级浅合并。
			l.Args = providers.MergeSettingsArg(l.Args, map[string]any{"env": inner})
		}
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

// composeResume 恢复会话：主题 + 权限，**不**注入模型（沿用该会话原模型）。
func composeResume(l providers.Launch, p providers.Provider, tool discovery.Tool, to ThemeOptions, po PermissionOptions) providers.Launch {
	return prependBinArgs(applyPermission(applyTheme(l, p, to), p, po), tool)
}

// ForSession 根据会话找到对应 provider 与可执行文件，产出恢复会话的启动描述。
// tools 为 discovery.DetectAll 的产物；会话所属工具不在表内或不可执行时报 ErrToolNotRunnable。
// mo 保留签名兼容调用方，恢复路径故意忽略（见 composeResume）。
func ForSession(ps []providers.Provider, tools []discovery.Tool, s providers.Session, to ThemeOptions, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	_ = mo
	tool, ok := toolFor(tools, s.ToolID)
	if ok && tool.Updating {
		return providers.Launch{}, ErrToolUpdating
	}
	if !ok || !tool.Installed || tool.BinPath == "" {
		return providers.Launch{}, ErrToolNotRunnable
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	return composeResume(p.ResumeCmd(s, tool.BinPath), p, tool, to, po), nil
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
	if ok && tool.Updating {
		return providers.Launch{}, ErrToolUpdating
	}
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
var ErrACPUnavailable = errors.New("err.launch.no_acp_adapter")

// ForSessionACP 产出用 ACP 恢复历史会话的启动描述（命令来自适配器探测）。
// mo 保留签名兼容调用方，恢复路径故意不注入模型。
func ForSessionACP(ps []providers.Provider, tools []discovery.Tool, s providers.Session, mo ModelOptions, po PermissionOptions) (providers.Launch, error) {
	_ = mo
	tool, ok := toolFor(tools, s.ToolID)
	if ok && tool.Updating {
		return providers.Launch{}, ErrToolUpdating
	}
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	// 与 ForWorkspaceACP 同款门禁：工具没有可执行入口时放行必在首条 prompt 时崩。
	// cursor 例外：官方通道能从已知位置（根目录 shim / PATH）兜底解析入口——
	// 更新器清空 versions\<ver>\node.exe 的窗口里 discovery 会给出 BinPath=""，
	// 但根目录 shim 通常仍在，此时恢复会话依然可用（见 officialCursorACP）。
	if !tool.Installed || tool.BinPath == "" {
		if !(tool.ID == "cursor" && cursorOfficialResolvable(tool)) {
			return providers.Launch{}, ErrToolNotRunnable
		}
	}
	p, ok := providerFor(ps, s.ToolID)
	if !ok {
		return providers.Launch{}, ErrToolNotRunnable
	}
	l, err := acpLaunch(tool, s.Workspace)
	if err != nil {
		return providers.Launch{}, err
	}
	return finalizeACP(l, p, tool, mo, po), nil
}

// finalizeACP 与 finalize 同构，但 ACP 适配器命令有 npx 形态：
// 注入参数前置后需重排到 -y <package> 之后（否则 npm 把模型名当包名）。
func finalizeACP(l providers.Launch, p providers.Provider, tool discovery.Tool, mo ModelOptions, po PermissionOptions) providers.Launch {
	baseLen := len(l.Args)
	if tool.ID == "cursor" {
		// 官方 acp 子命令不接受外部模型名（`--model <自定义名>` 进程即退出
		// "Cannot use this model"）；cursor 的模型切换走会话内 session/set_model。
		// 旧适配器回退路径同样不注入：cursor-acp 0.1.0 本就不透传 --model
		//（"not wired in v1"），注入与不注入无行为差别，统一跳过防误配。
		mo = ModelOptions{}
	}
	out := finalize(l, p, mo, po)
	out.ACPSettingsJSON = acpSettingsPayload(p, mo)
	return reorderACPArgs(out, tool, len(out.Args)-baseLen)
}

// acpSettingsPayload 为 claude ACP 组装完整的 _meta 载荷
// {"claudeCode":{"options":{"settings":"{\"env\":{...}}"}}}：适配器从
// params._meta.claudeCode.options 读 SDK options，settings 值经 SDK 原样
// 进 spawn argv → CLI 的 --settings（内联 JSON）。**settings 值必须是 JSON
// 字符串而非对象**——SDK 无 sandbox 时不序列化，对象会被 Node 变成
// "[object Object]" 致 CLI 启动失败。非 claude、Resolver 未命中或 env 全空
// 时返回空串（session/new 不带 _meta）。
func acpSettingsPayload(p providers.Provider, mo ModelOptions) string {
	if p.ID() != "claude" || mo.Resolver == nil {
		return ""
	}
	cfg, ok := mo.Resolver(p.ID())
	if !ok {
		return ""
	}
	env := providers.Claude{}.SettingsEnv(cfg)
	if len(env) == 0 {
		return ""
	}
	settingsJSON, err := json.Marshal(map[string]any{"env": env})
	if err != nil {
		return ""
	}
	b, err := json.Marshal(map[string]any{
		"claudeCode": map[string]any{
			"options": map[string]any{"settings": string(settingsJSON)},
		},
	})
	if err != nil {
		return ""
	}
	return string(b)
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
		if ok && t.Updating {
			return providers.Launch{}, ErrToolUpdating
		}
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		pp, ok := providerFor(ps, toolID)
		if !ok {
			return providers.Launch{}, ErrToolNotRunnable
		}
		tool, p = t, pp
	}
	// 新建 ACP 的登录门禁：放在两条选路（显式 toolID 与 PreferredTool）汇合之后、
	// acpLaunch 之前，保证 PreferredTool 路径同样被覆盖。只拦新建；恢复
	//（ForSessionACP）与终端（ForWorkspaceTool）路径按用户决策放行。unknown
	// 必须保守放行——探测不稳定时宁可多跑一次失败的 ACP，不能错杀已登录用户。
	if tool.Auth == string(providers.AuthOut) {
		return providers.Launch{}, ErrNotLoggedIn
	}
	l, err := acpLaunch(tool, ws.Path)
	if err != nil {
		return providers.Launch{}, err
	}
	return finalizeACP(l, p, tool, mo, po), nil
}

// gitBashEnvFn 抽象 claudeenv.GitBashEnv，测试可替换。
var gitBashEnvFn = claudeenv.GitBashEnv

// acpLaunch 按适配器探测结果组装 ACP 启动描述。
func acpLaunch(tool discovery.Tool, dir string) (providers.Launch, error) {
	// cursor 优先官方直连：cursor-agent 原生支持 acp 子命令（持久 ACP 服务，
	// 真会话记忆），无需第三方 cursor-acp 适配器。官方不可用（shim 且解析不到
	// node/脚本）才回退适配器路径。
	if tool.ID == "cursor" {
		if l, ok := officialCursorACP(tool); ok {
			l.Dir = dir
			return l, nil
		}
	}
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
	if tool.ID == "claude" {
		// claude CLI 在 Windows 强依赖 git-bash 且只探测标准安装路径；
		// Git 装在非标准位置时给适配器子进程补 CLAUDE_CODE_GIT_BASH_PATH，
		// 否则 session/new 阶段 CLI 启动即退出（Query closed before response received）。
		if p, ok := gitBashEnvFn(); ok {
			l.Env = providers.MergeEnv(l.Env, map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": p})
		}
	}
	return l, nil
}

// cursorRootShimFn 兜底解析 cursor-agent 入口：discovery 在更新器清空
// versions\<ver>\node.exe 的窗口里会给出 BinPath=""（Broken 形态），但根目录
// shim（cursor-agent.cmd / agent.cmd）通常仍在。PATH 只探测 cursor-agent 名
//（"agent" 太宽泛，仅限官方安装根内匹配），再扫 Windows 官方安装根。测试可替换。
var cursorRootShimFn = func() (string, bool) {
	for _, name := range []string{"cursor-agent", "cursor-agent.exe"} {
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p, true
		}
	}
	if runtime.GOOS != "windows" {
		return "", false
	}
	root := filepath.Join(os.Getenv("LOCALAPPDATA"), "cursor-agent")
	for _, name := range []string{"cursor-agent.cmd", "agent.cmd", "cursor-agent.exe", "agent.exe"} {
		p := filepath.Join(root, name)
		if isFile(p) {
			return p, true
		}
	}
	return "", false
}

// cursorOfficialResolvable 报告 cursor 官方 ACP 入口能否解析（含兜底探测）。
func cursorOfficialResolvable(tool discovery.Tool) bool {
	_, ok := officialCursorACP(tool)
	return ok
}

// cursorNodeEntryFn 第二级兜底：根目录 shim 也被更新器删光时（只留
// versions\<ver>\index.js 的极端窗口），用系统 node 直接跑最新版本的
// index.js——官方更新器从不删版本目录内的 index.js 本体。测试可替换。
var cursorNodeEntryFn = func() (string, bool) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return "", false
	}
	for _, dir := range cursorVersionDirs(filepath.Join(root, "cursor-agent")) {
		script := filepath.Join(dir, "index.js")
		if isFile(script) {
			return script, true
		}
	}
	return "", false
}

// officialCursorACP 解析 cursor-agent 本体直连的 ACP 启动描述（官方 `acp` 子命令）。
// 入口形态：node 入口（BinPath=node.exe，BinArgs=[index.js]）直接
// `node index.js acp`；.cmd/.bat shim 先解析 node 与 versions\<ver>\index.js，
// 解析不到返回 false（走适配器回退）；原生二进制（含 macOS/Linux）直接 `本体 acp`。
// BinPath 为空（更新器清空入口的窗口）时依次经 cursorRootShimFn、
// cursorNodeEntryFn 兜底。kshell 自身即 ACP 客户端，无需代理三元组。
func officialCursorACP(tool discovery.Tool) (providers.Launch, bool) {
	binPath, binArgs := tool.BinPath, tool.BinArgs
	if binPath == "" {
		if p, ok := cursorRootShimFn(); ok {
			binPath = p
		} else if script, ok := cursorNodeEntryFn(); ok {
			node, ok2 := nodeLookPathFn()
			if !ok2 {
				return providers.Launch{}, false
			}
			return providers.Launch{Path: node, Args: []string{script, "acp"}}, true
		} else {
			return providers.Launch{}, false
		}
	}
	if len(binArgs) > 0 {
		return providers.Launch{Path: binPath, Args: []string{binArgs[0], "acp"}}, true
	}
	if isCmdShim(binPath) {
		node, ok := nodeLookPathFn()
		if !ok {
			return providers.Launch{}, false
		}
		script, ok := cursorScriptResolveFn(binPath)
		if !ok {
			return providers.Launch{}, false
		}
		return providers.Launch{Path: node, Args: []string{script, "acp"}}, true
	}
	return providers.Launch{Path: binPath, Args: []string{"acp"}}, true
}

// reorderACPArgs 把 finalize 前置进 Args 的注入参数挪到 npx 包名之后：
// npx 命令行里包名必须紧跟 -y，注入参数（--model 等）若插在包名前会被
// npm 当成待安装的包名（E404）。插入点固定在 -y 与包名之后（ExtraArgs 之前），
// ExtraArgs 是适配器自身的旗子，注入参数与它谁先谁后无语义差别。
// path 形态无包名问题，原样返回。
func reorderACPArgs(l providers.Launch, tool discovery.Tool, injectLen int) providers.Launch {
	if tool.ACP == nil || tool.ACP.Source != "npx" || injectLen <= 0 {
		return l
	}
	if len(l.Args) < injectLen {
		return l
	}
	injected := l.Args[:injectLen]
	base := l.Args[injectLen:]
	// base 形如 [-y, pkg, ...ExtraArgs]；插入点在 -y 与 pkg 之后
	at := 2
	if at > len(base) {
		at = len(base)
	}
	out := make([]string, 0, len(l.Args))
	out = append(out, base[:at]...)
	out = append(out, injected...)
	out = append(out, base[at:]...)
	l.Args = out
	return l
}

// acpAdapterEnv 为依赖本体 CLI 的适配器补充环境变量。目前只有 cursor-acp：
// cursor-agent 常不在 PATH 上（Windows 装在 %LOCALAPPDATA%\cursor-agent），
// 适配器默认只按 PATH 查找，须显式传 CURSOR_AGENT_EXECUTABLE。
// 两类路径无法被 Node 直接 spawn 成单一文件：node 入口形态、以及 .cmd/.bat shim
//（Node ≥20.12 出于 CVE-2024-27980 对 .cmd 同步抛 EINVAL，cursor-acp 的 spawn
// 不带 shell:true，prompt 即崩 -32603）。两者都改把当前 kshell 可执行文件当代理，
// 由代理再 exec node + index.js。
func acpAdapterEnv(tool discovery.Tool) map[string]string {
	if tool.ID != "cursor" || tool.BinPath == "" {
		return nil
	}
	if len(tool.BinArgs) == 0 {
		// .cmd/.bat shim：改走代理形态；node、脚本或自身路径解析不到时一律
		// 退回直注 shim（保持既有行为，至少老 Node 环境仍可用）。
		if isCmdShim(tool.BinPath) {
			fallback := map[string]string{"CURSOR_AGENT_EXECUTABLE": tool.BinPath}
			node, ok := nodeLookPathFn()
			if !ok {
				return fallback
			}
			script, ok := cursorScriptResolveFn(tool.BinPath)
			if !ok {
				return fallback
			}
			if env := proxyEnv(node, script); env != nil {
				return env
			}
			return fallback
		}
		return map[string]string{"CURSOR_AGENT_EXECUTABLE": tool.BinPath}
	}
	return proxyEnv(tool.BinPath, tool.BinArgs[0])
}

// proxyEnv 组装 kshell 代理三元组：cursor-acp spawn kshell 自身，
// kshell 进程入口看到 KSHELL_AS_CURSOR_AGENT 后转 exec node + 脚本。
func proxyEnv(node, script string) map[string]string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// 解析不到自身路径时无法提供可 spawn 的 PE，退回不注入（cursor-acp 仍会 ENOENT）。
		return nil
	}
	return map[string]string{
		"CURSOR_AGENT_EXECUTABLE": exe,
		cursoragent.EnvAsProxy:    "1",
		cursoragent.EnvNode:       node,
		cursoragent.EnvScript:     script,
	}
}

// isCmdShim 判断路径是否为 .cmd/.bat 批处理 shim（Node spawn 会 EINVAL 的形态）。
func isCmdShim(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cmd" || ext == ".bat"
}

// nodeLookPathFn 抽象 node.exe 查找，测试可替换。
var nodeLookPathFn = func() (string, bool) {
	p, err := exec.LookPath("node.exe")
	if err != nil || p == "" {
		return "", false
	}
	return p, true
}

// cursorScriptResolveFn 抽象「从 shim 路径解析 index.js」：shim 在安装根
// （如 %LOCALAPPDATA%\cursor-agent\cursor-agent.cmd），本体脚本在
// versions\<ver>\index.js（最新版本在前，与 providers 的版本排序同规则），
// 更新器删光版本目录时兜底根目录 index.js。测试可替换。
var cursorScriptResolveFn = func(shimPath string) (string, bool) {
	abs, err := filepath.Abs(shimPath)
	if err != nil {
		return "", false
	}
	root := filepath.Dir(abs)
	if root == "" {
		return "", false
	}
	for _, dir := range cursorVersionDirs(root) {
		script := filepath.Join(dir, "index.js")
		if isFile(script) {
			return script, true
		}
	}
	script := filepath.Join(root, "index.js")
	return script, isFile(script)
}

// cursorVersionDirs 列出安装根下 versions\<ver> 子目录，最新版本在前。
// 目录名形如 YYYY.MM.DD-commit 或 YYYY.MM.DD-HH-MM-SS-commit，按 6 段数值
// 降序排序（月份不补零时字典序会跨月错序）。非法目录名排在最后兜底。
func cursorVersionDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		return nil
	}
	type versioned struct {
		path string
		ver  [6]int
		name string
	}
	var valid []versioned
	var invalid []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.Contains(name, ".") || !strings.Contains(name, "-") {
			continue
		}
		if ver, ok := parseVersionDir(name); ok {
			valid = append(valid, versioned{path: filepath.Join(root, "versions", name), ver: ver, name: name})
			continue
		}
		invalid = append(invalid, filepath.Join(root, "versions", name))
	}
	sort.SliceStable(valid, func(i, j int) bool {
		for k := 0; k < 6; k++ {
			if valid[i].ver[k] != valid[j].ver[k] {
				return valid[i].ver[k] > valid[j].ver[k]
			}
		}
		return valid[i].name > valid[j].name
	})
	sort.Sort(sort.Reverse(sort.StringSlice(invalid)))
	out := make([]string, 0, len(valid)+len(invalid))
	for _, v := range valid {
		out = append(out, v.path)
	}
	return append(out, invalid...)
}

// parseVersionDir 解析 versions 目录名为 6 段数值版本号（缺时分秒补 0）。
func parseVersionDir(name string) ([6]int, bool) {
	var ver [6]int
	segs := strings.Split(name, "-")
	if len(segs) != 2 && len(segs) != 5 {
		return ver, false
	}
	date := strings.Split(segs[0], ".")
	if len(date) != 3 {
		return ver, false
	}
	nums := append([]string{}, date...)
	if len(segs) == 5 {
		nums = append(nums, segs[1], segs[2], segs[3])
	}
	for i, s := range nums {
		n, err := strconv.Atoi(s)
		if err != nil {
			return ver, false
		}
		ver[i] = n
	}
	return ver, true
}

// isFile 判断路径存在且为普通文件。
func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
