package desktop

import (
	"fmt"
	"os"
	"runtime"

	"github.com/yangk/kshell/internal/terminal"
)

// OpenShellTerminal 在预览区为工作区新开一个本地 shell 终端（每次独立进程）。
func (a *App) OpenShellTerminal(wsID string, cols, rows int) (terminal.Info, error) {
	ws, _, ok := a.workspaceByIDReady(wsID)
	if !ok {
		return terminal.Info{}, errWorkspaceNotFound
	}
	m := a.terminals()
	if m == nil {
		return terminal.Info{}, errNotReady
	}

	path, args := localShellSpec(ws.Path)
	key := fmt.Sprintf("shell:%d", termKeySeq.Add(1))
	return m.Open(key, terminal.Info{
		Kind:      terminal.KindShell,
		Workspace: ws.Path,
		Title:     "终端",
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
