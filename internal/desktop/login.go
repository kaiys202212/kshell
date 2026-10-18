package desktop

import (
	"errors"
	"os"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/terminal"
)

// errLoginUnsupported 表示该工具没有官方交互式登录命令（未实现
// LoginCmdProvider）、LoginCmd 为空或 toolID 不在 provider 表中：
// 前端登录弹窗据此提示「不支持」，不当异常栈展示。
var errLoginUnsupported = errors.New("err.login.unsupported")

// userHomeDir 独立成变量：登录终端的启动目录取用户主目录，测试可打桩。
var userHomeDir = os.UserHomeDir

// toolsSnapshot 加锁拷贝工具探测表。与 GetTools 不同，刻意不走
// ensureToolsReady（与 CheckToolAuth 同理）：登录弹窗打开时不应等首轮扫描，
// 探测缺失时调用方回退裸命令即可。
func (a *App) toolsSnapshot() []discovery.Tool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tools
}

// OpenLoginTerminal 为指定工具打开一个跑官方交互式登录命令的独立终端
// （登录弹窗专用，如 cursor-agent login 的交互式 OAuth 流程）。
// 与 CheckToolAuth 同理刻意不走 ensureToolsReady：provider 表启动装配即可用，
// 弹窗打开时不应等首轮扫描。
//
// key 固定 login:<toolID>：同工具的登录终端全局唯一，运行中重复打开直接复用
// （弹窗可能重复触发，绝不能起第二个登录进程）；已退出时由 Manager.Open
// 原地重启，覆盖旧会话即可，无需先 Close。
func (a *App) OpenLoginTerminal(toolID string, cols, rows int) (terminal.Info, error) {
	o := a.snapshot()
	var cmd []string
	display := ""
	for _, p := range o.Providers {
		if p.ID() != toolID {
			continue
		}
		display = p.DisplayName()
		if lp, ok := p.(providers.LoginCmdProvider); ok {
			cmd = lp.LoginCmd()
		}
		break
	}
	if len(cmd) == 0 {
		return terminal.Info{}, errLoginUnsupported
	}
	m := a.terminals()
	if m == nil {
		return terminal.Info{}, errNotReady
	}

	// 登录与工作区无关，工具把登录态写进自己的用户级配置，启动目录取用户
	// 主目录；拿不到就回退空串（终端后端继承当前目录），不影响登录流程。
	home, err := userHomeDir()
	if err != nil {
		home = ""
	}
	// 优先复用探测结果：按 toolID 找 discovery.Tool，BinPath 非空时用探测到的
	// 可执行入口（含 node 入口形态的 BinArgs 前缀）拼 login 子命令，与工作区
	// 会话同等的启动能力——官方安装目录往往不在 GUI 进程 PATH（Cursor 根
	// shim 也已改名 agent.{cmd,ps1}），裸命令会在 terminal 后端 LookPath 失败。
	// BinPath 为空（未装/探测缺失）时回退 LoginCmd 裸命令，PATH 命中的安装仍可用。
	var tool discovery.Tool
	for _, t := range a.toolsSnapshot() {
		if t.ID == toolID {
			tool = t
			break
		}
	}
	l := providers.Launch{Dir: home}
	if tool.BinPath != "" {
		l.Path = tool.BinPath
		l.Args = append(append([]string(nil), tool.BinArgs...), cmd[1:]...)
	} else {
		l.Path = cmd[0]
		l.Args = append([]string(nil), cmd[1:]...)
	}
	// 与 OpenWorkspaceTerminal 同构：统一经 launcher.Build 做校验与 shim 解析
	// （Windows 上 npm 安装的 .ps1 入口不能直接 exec，转为 .cmd 兄弟文件或
	// powershell）。注意裸名对这层是 no-op，真正的 PATH 解析兜底在 terminal
	// 后端 commandLine 的 exec.LookPath。
	spec, err := launcher.Build(l)
	if err != nil {
		return terminal.Info{}, err
	}

	title := display
	if title == "" {
		title = toolID
	}
	return m.Open("login:"+toolID, terminal.Info{
		Kind:   terminal.KindLogin,
		ToolID: toolID,
		Title:  title + " " + applang.T("terminal.login_title"),
	}, terminal.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, cols, rows)
}
