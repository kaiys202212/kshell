package desktop

import (
	"errors"
	"os"

	"github.com/yangk/kshell/internal/applang"
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

// OpenLoginTerminal 为指定工具打开一个跑官方交互式登录命令的独立终端
// （登录弹窗专用，如 cursor-agent login 的交互式 OAuth 流程）。
// 与 CheckToolAuth 同理刻意不走 ensureToolsReady：provider 表启动装配即可用，
// 弹窗打开时不应等首轮扫描。
//
// key 固定 login:<toolID>：同工具的登录终端全局唯一，运行中重复打开直接复用
// （弹窗可能重复触发，绝不能起第二个登录进程）；已退出时由 Manager.Open
// 原地重启，覆盖旧会话即可，无需先 Close。
func (a *App) OpenLoginTerminal(toolID string, cols, rows int) (terminal.Info, error) {
	var cmd []string
	display := ""
	for _, p := range a.snapshot().Providers {
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
	// 与 OpenWorkspaceTerminal 同构：统一经 launcher.Build 做校验与 shim 解析。
	// Windows 上 npm 安装的 CLI 入口常是 .ps1/.cmd，不能绕过这层直接 exec。
	spec, err := launcher.Build(providers.Launch{Path: cmd[0], Args: cmd[1:], Dir: home})
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
