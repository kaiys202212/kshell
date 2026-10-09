package desktop

import (
	"fmt"
	"os"
	"runtime"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/terminal"
)

// OpenShellTerminal 在预览区为工作区新开一个 shell 终端（每次独立进程）。
// 本地工作区起本机 shell；ssh Ref 则 OpenSSH 并 cd 到远端路径。
func (a *App) OpenShellTerminal(wsID string, cols, rows int) (terminal.Info, error) {
	ws, _, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return terminal.Info{}, errWorkspaceNotFound
	}
	m := a.terminals()
	if m == nil {
		return terminal.Info{}, errNotReady
	}

	kind, connID, remotePath, err := discovery.ParseWorkspaceRef(ws.Path)
	if err != nil {
		return terminal.Info{}, err
	}
	if kind == discovery.KindSSH {
		c, ok := a.connByID(connID)
		if !ok {
			return terminal.Info{}, errConnNotFound
		}
		l, err := a.buildSSHInteractiveLaunch(c, sshShellCdCommand(remotePath))
		if err != nil {
			return terminal.Info{}, err
		}
		spec, err := launcher.Build(l)
		if err != nil {
			return terminal.Info{}, err
		}
		key := fmt.Sprintf("ssh:%s:%d", c.ID, termKeySeq.Add(1))
		title := c.Name
		if title == "" {
			title = c.Target()
		}
		return m.Open(key, terminal.Info{
			Kind:      terminal.KindSSH,
			ConnID:    c.ID,
			Workspace: ws.Path,
			Title:     title,
		}, terminal.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, cols, rows)
	}

	path, args := localShellSpec(ws.Path)
	key := fmt.Sprintf("shell:%d", termKeySeq.Add(1))
	return m.Open(key, terminal.Info{
		Kind:      terminal.KindShell,
		Workspace: ws.Path,
		Title:     applang.T("terminal.title"),
	}, terminal.Spec{Path: path, Args: args, Dir: ws.Path}, cols, rows)
}

// localShellSpec 返回本机交互式 shell 可执行文件与参数。
// dir 是启动目录：进程 cwd 设为它；Windows 上 PowerShell 配置文件常会再 cd，
// 因此再用 -NoExit -Command Set-Location 钉回工作区。
func localShellSpec(dir string) (path string, args []string) {
	if runtime.GOOS == "windows" {
		script := "Set-Location -LiteralPath " + psQuote(dir)
		return "powershell", []string{"-NoLogo", "-NoExit", "-Command", script}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, nil
	}
	return "/bin/bash", nil
}
