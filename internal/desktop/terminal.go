package desktop

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// 终端相关错误：绑定层只做参数组装与错误转发，业务错误由 terminal/launch 包给出。
var errBadTerminalData = errors.New("终端输入不是合法的 base64 数据")

// termKeySeq 给「工作区新建终端」生成唯一 key（每次新建都要起新进程，不能复用）。
// 全局自增即可：key 只在同一个 Manager 内需要唯一。
var termKeySeq atomic.Int64

// newTerminalManager 用真实后端装配终端管理器（initRealDeps 用）。
func newTerminalManager(a *App) *terminal.Manager {
	m := newTerminalManagerWith(a.Emit, terminal.NewPTYBackend())
	// Gemini 无 hooks：终端 OSC 9 / 777;notify 命中直接复用 handleAgentNotify
	//（emit notify:agent + 窗口隐藏时补 toast），与 hooks 通道同一条分发路径。
	m.SetOnNotify(a.handleAgentNotify)
	return m
}

// newTerminalManagerWith 装配终端管理器：输出/退出经 emit 转发给前端。
// emit 可能尚未就绪（Emit 自带 nil 兜底），回调总在 Manager 锁外执行。
func newTerminalManagerWith(emit func(name string, data ...any), b terminal.Backend) *terminal.Manager {
	return terminal.NewManager(b,
		func(id string, chunk []byte) {
			emit("terminal:data", map[string]any{
				"id":   id,
				"data": base64.StdEncoding.EncodeToString(chunk),
			})
		},
		func(id string, exitCode int) {
			emit("terminal:exit", map[string]any{"id": id, "exitCode": exitCode})
		})
}

// terminals 返回终端管理器；未装配（未走 Startup）时返回 nil。
func (a *App) terminals() *terminal.Manager {
	return a.snapshot().Terminals
}

// OpenSessionTerminal 在中心区打开（已打开则复用）某历史会话的内嵌终端。
// key 与会话 ID 绑定，因此重复恢复同一会话不会起第二个进程。
func (a *App) OpenSessionTerminal(sessionID string, cols, rows int) (terminal.Info, error) {
	a.ensureSessionHooks()
	s, tools, ok := a.sessionByIDReady(sessionID)
	if !ok {
		return terminal.Info{}, errSessionNotFound
	}
	m := a.terminals()
	if m == nil {
		return terminal.Info{}, errNotReady
	}

	o := a.snapshot()
	l, err := launch.ForSession(o.Providers, tools, s, a.themeOptions(), a.modelOptions(), a.permissionOptions())
	if err != nil {
		return terminal.Info{}, err
	}
	spec, err := launcher.Build(l)
	if err != nil {
		return terminal.Info{}, err
	}
	// 注入 agent 通知归因与 hook 参数；失败静默跳过，不影响会话启动
	applyNotifyInject(&spec, s.ToolID, "session:"+sessionID, s.Workspace)

	return m.Open("session:"+sessionID, terminal.Info{
		Kind:      terminal.KindSession,
		SessionID: s.ID,
		Workspace: s.Workspace,
		Title:     s.Title,
		ToolID:    s.ToolID,
	}, terminal.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, cols, rows)
}

// OpenWorkspaceTerminal 在中心区为工作区新开一个内嵌终端；toolID 为空时用工作区首选工具。
func (a *App) OpenWorkspaceTerminal(wsID string, toolID string, cols, rows int) (terminal.Info, error) {
	a.ensureSessionHooks()
	ws, tools, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return terminal.Info{}, errWorkspaceNotFound
	}
	m := a.terminals()
	if m == nil {
		return terminal.Info{}, errNotReady
	}

	o := a.snapshot()
	l, err := launch.ForWorkspaceTool(o.Providers, tools, ws, toolID, a.themeOptions(), a.modelOptions(), a.permissionOptions())
	if err != nil {
		return terminal.Info{}, err
	}
	spec, err := launcher.Build(l)
	if err != nil {
		return terminal.Info{}, err
	}

	key := fmt.Sprintf("new:%d", termKeySeq.Add(1))
	// toolID 为空时 launch 选了首选工具：还原出真实工具 ID 才能按工具注入 hook
	injectToolID := toolID
	if injectToolID == "" {
		if p, _, ok := launch.PreferredTool(o.Providers, tools, ws); ok {
			injectToolID = p.ID()
		}
	}
	applyNotifyInject(&spec, injectToolID, key, ws.Path)

	info, err := m.Open(key, terminal.Info{
		Kind:            terminal.KindNew,
		Workspace:       ws.Path,
		Title:           workspaceTerminalTitle(o.Providers, tools, ws, toolID),
		ToolID:          injectToolID, // 还原后的真实工具 ID，页签展示与通知注入同源
		KnownSessionIDs: a.knownSessionIDs(),
	}, terminal.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, cols, rows)
	if err != nil {
		return terminal.Info{}, err
	}
	return info, nil
}

// WriteTerminal 把前端输入写进终端；data 是 base64（xterm 的 onData 可能含任意字节）。
func (a *App) WriteTerminal(id string, data string) error {
	a.ensureSessionHooks()
	m := a.terminals()
	if m == nil {
		return errNotReady
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return errBadTerminalData
	}
	return m.Write(id, raw)
}

// ResizeTerminal 调整终端尺寸（前端 FitAddon 变化时调用）。
func (a *App) ResizeTerminal(id string, cols, rows int) error {
	m := a.terminals()
	if m == nil {
		return errNotReady
	}
	return m.Resize(id, cols, rows)
}

// CloseTerminal 关闭终端（幂等），前端关闭终端页签时调用。
func (a *App) CloseTerminal(id string) error {
	m := a.terminals()
	if m == nil {
		return errNotReady
	}
	return m.Close(id)
}

// ListTerminals 返回全部终端快照，供前端还原页签。
func (a *App) ListTerminals() []terminal.Info {
	m := a.terminals()
	if m == nil {
		return []terminal.Info{}
	}
	return m.List()
}

// ScrollbackTerminal 返回终端最近输出的回放数据（base64），已退出的终端仍可读。
func (a *App) ScrollbackTerminal(id string) (string, error) {
	m := a.terminals()
	if m == nil {
		return "", errNotReady
	}
	data, err := m.Scrollback(id)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// NewSessionWithTool 在指定工作区用指定工具新建会话（外部终端窗口路径）；
// toolID 为空时等价于 NewSession（交给 launch 选首选工具）。
func (a *App) NewSessionWithTool(wsID string, toolID string) error {
	ws, tools, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return errWorkspaceNotFound
	}
	o := a.snapshot()
	l, err := launch.ForWorkspaceTool(o.Providers, tools, ws, toolID, a.themeOptions(), a.modelOptions(), a.permissionOptions())
	if err != nil {
		return err
	}
	return a.launchWindow(o.Windows, l, toolID, ws.Name)
}

// workspaceTerminalTitle 生成内嵌终端页签标题：工作区名 + 工具展示名。
// toolID 为空时按首选工具取展示名；取不到名字时只用工作区名。
func workspaceTerminalTitle(ps []providers.Provider, tools []discovery.Tool, ws discovery.Workspace, toolID string) string {
	name := ""
	if toolID != "" {
		for _, t := range tools {
			if t.ID == toolID {
				name = t.Name
				break
			}
		}
	} else if _, tool, ok := launch.PreferredTool(ps, tools, ws); ok {
		name = tool.Name
	}
	if name == "" {
		return ws.Name
	}
	return ws.Name + " · " + name
}
