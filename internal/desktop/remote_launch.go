package desktop

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
)

// isCursorAgentBin 判断本机 bin 是否为 cursor-agent（CLI），而非 IDE/`cursor` 启动器。
func isCursorAgentBin(bin string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(bin)))
	return strings.Contains(base, "cursor-agent")
}

func (a *App) findSSH() (string, error) {
	if fn := a.snapshot().FindSSH; fn != nil {
		return fn()
	}
	return remote.FindSSH()
}

// remoteWhich 远端 command -v；失败返回空串。
func (a *App) remoteWhich(conn remote.Connection, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty bin name")
	}
	ctx, cancel := context.WithTimeout(a.sshCtx(), 15*time.Second)
	defer cancel()
	cmd := fmt.Sprintf("command -v %s", shellSingleQuoteRemote(name))
	stdout, _, err := a.remoteRunner()(ctx, conn, cmd, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}

func shellSingleQuoteRemote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildSSHInteractiveLaunch 组装带 -t 的交互式 ssh Launch（远端跑 CLI / shell）。
func (a *App) buildSSHInteractiveLaunch(conn remote.Connection, remoteCmd string) (providers.Launch, error) {
	bin, err := a.findSSH()
	if err != nil {
		return providers.Launch{}, err
	}
	args := remote.ShellArgs(conn, a.sshOptions())
	if strings.TrimSpace(remoteCmd) != "" {
		args = append(args, "--", remoteCmd)
	}
	env, err := remote.AskPassEnvMap(conn.Password)
	if err != nil {
		return providers.Launch{}, err
	}
	return providers.Launch{Path: bin, Args: args, Env: env}, nil
}

// resolveRemoteLaunch 按规格 §6：RemoteLauncher（本机 bin）→ ssh 远端 CLI → tool_not_found。
// s 非 nil 为恢复；nil 为新建。返回选用的 toolID（空 toolID 入参时可能被选中）。
func (a *App) resolveRemoteLaunch(
	conn remote.Connection,
	remotePath string,
	toolID string,
	s *providers.Session,
	tools []discovery.Tool,
) (providers.Launch, string, error) {
	ps := a.snapshot().Providers
	candidates := a.remoteToolCandidates(ps, tools, toolID)
	if len(candidates) == 0 {
		id := toolID
		if id == "" {
			id = "unknown"
		}
		return providers.Launch{}, "", providers.ErrRemoteToolNotFound(id)
	}

	var last error
	for _, c := range candidates {
		l, err := a.tryRemoteLaunch(c.p, c.tool, conn, remotePath, s)
		if err == nil {
			return l, c.p.ID(), nil
		}
		last = err
	}
	if last != nil {
		return providers.Launch{}, "", last
	}
	return providers.Launch{}, "", providers.ErrRemoteToolNotFound(candidates[0].p.ID())
}

type remoteToolCand struct {
	p    providers.Provider
	tool discovery.Tool
}

func (a *App) remoteToolCandidates(ps []providers.Provider, tools []discovery.Tool, toolID string) []remoteToolCand {
	if toolID != "" {
		p, ok := providerByID(ps, toolID)
		if !ok {
			return nil
		}
		tool, _ := toolByID(tools, toolID)
		return []remoteToolCand{{p: p, tool: tool}}
	}

	var out []remoteToolCand
	seen := map[string]bool{}
	add := func(p providers.Provider, tool discovery.Tool) {
		if seen[p.ID()] {
			return
		}
		seen[p.ID()] = true
		out = append(out, remoteToolCand{p: p, tool: tool})
	}

	// 1) 有官方协议且本机 bin 可用的优先（如 Cursor）
	for _, p := range ps {
		if _, ok := p.(providers.RemoteLauncher); !ok {
			continue
		}
		tool, ok := toolByID(tools, p.ID())
		if ok && tool.Installed && tool.BinPath != "" {
			add(p, tool)
		}
	}
	// 2) 本机已安装的首选顺序
	if p, tool, ok := launch.PreferredTool(ps, tools, discovery.Workspace{}); ok {
		add(p, tool)
	}
	for _, p := range ps {
		tool, ok := toolByID(tools, p.ID())
		if ok && tool.Installed && tool.BinPath != "" {
			add(p, tool)
		}
	}
	// 3) 仅远端 CLI 的候选（本机未装）
	for _, p := range ps {
		if _, ok := p.(providers.RemoteSSHRunner); ok {
			tool, _ := toolByID(tools, p.ID())
			add(p, tool)
		}
	}
	return out
}

func (a *App) tryRemoteLaunch(
	p providers.Provider,
	tool discovery.Tool,
	conn remote.Connection,
	remotePath string,
	s *providers.Session,
) (providers.Launch, error) {
	// 官方协议：本机 bin 可用。Cursor 的 cursor-agent 不支持 --folder-uri，跳过走 ssh。
	if rl, ok := p.(providers.RemoteLauncher); ok && tool.Installed && strings.TrimSpace(tool.BinPath) != "" {
		bin := tool.BinPath
		if !(p.ID() == "cursor" && isCursorAgentBin(bin)) {
			if s == nil {
				return rl.NewRemoteSessionCmd(conn.Target(), remotePath, bin)
			}
			return rl.ResumeRemoteCmd(conn.Target(), remotePath, *s, bin)
		}
	}

	runner, ok := p.(providers.RemoteSSHRunner)
	if !ok {
		return providers.Launch{}, providers.ErrRemoteToolNotFound(p.ID())
	}
	binName := p.DetectSpec("").BinName
	if binName == "" {
		binName = p.ID()
	}
	remoteBin, err := a.remoteWhich(conn, binName)
	if err != nil || remoteBin == "" {
		return providers.Launch{}, providers.ErrRemoteToolNotFound(p.ID())
	}
	var args []string
	if s == nil {
		args = runner.RemoteNewArgs(remotePath)
	} else {
		args = runner.RemoteResumeArgs(*s)
	}
	cmd := providers.RemoteShellCommand(remotePath, remoteBin, args)
	return a.buildSSHInteractiveLaunch(conn, cmd)
}

func providerByID(ps []providers.Provider, id string) (providers.Provider, bool) {
	for _, p := range ps {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

func toolByID(tools []discovery.Tool, id string) (discovery.Tool, bool) {
	for _, t := range tools {
		if t.ID == id {
			return t, true
		}
	}
	return discovery.Tool{}, false
}

// parseSSHWorkspace 若 wsPath 是 ssh Ref 则返回连接与远端路径。
func (a *App) parseSSHWorkspace(wsPath string) (remote.Connection, string, bool, error) {
	kind, connID, remotePath, err := discovery.ParseWorkspaceRef(wsPath)
	if err != nil {
		return remote.Connection{}, "", false, err
	}
	if kind != discovery.KindSSH {
		return remote.Connection{}, "", false, nil
	}
	c, ok := a.connByID(connID)
	if !ok {
		return remote.Connection{}, "", false, errConnNotFound
	}
	return c, remotePath, true, nil
}

// sshShellCdCommand 登录后 cd 到远端路径再起交互 shell。
func sshShellCdCommand(remotePath string) string {
	return "cd " + shellSingleQuoteRemote(remotePath) + ` && exec "${SHELL:-/bin/bash}" -l`
}

// synthesizeSSHWorkspace 项目表有登记但扫描列表尚未呈现时，拼一个可用 Workspace。
func synthesizeSSHWorkspace(connID, remotePath string) discovery.Workspace {
	ref := discovery.FormatSSHRef(connID, remotePath)
	name := path.Base(remotePath)
	if name == "" || name == "/" {
		name = remotePath
	}
	return discovery.Workspace{
		Path:       ref,
		Name:       name,
		Kind:       discovery.KindSSH,
		ConnID:     connID,
		RemotePath: remotePath,
	}
}
