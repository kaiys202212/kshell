package desktop

import (
	"context"
	"errors"
	"os"

	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
)

var errConnNotFound = errors.New("连接不存在")

// sshOptions 把配置里的 ssh 段转成 remote 执行参数（与 TUI 同口径）。
func (a *App) sshOptions() remote.SSHOptions {
	c := a.snapshot().Config.SSHOptions
	return remote.SSHOptions{
		ConnectTimeout:        c.ConnectTimeout,
		ExtraArgs:             c.ExtraArgs,
		CommandTimeoutSeconds: c.CommandTimeoutSeconds,
	}
}

// connByID 按连接 ID 查找（store 的连接列表由其内部一致性保证）。
func (a *App) connByID(connID string) (remote.Connection, bool) {
	store := a.snapshot().Store
	if store == nil {
		return remote.Connection{}, false
	}
	for _, c := range store.All() {
		if c.ID == connID {
			return c, true
		}
	}
	return remote.Connection{}, false
}

// ListConnections 返回连接列表：绑定到 wsID 工作区的 + 未绑定工作区的全局连接；
// wsID 为空时返回全部。store 内部状态由调用方串行访问，这里直接透传。
func (a *App) ListConnections(wsID string) []remote.Connection {
	store := a.snapshot().Store
	if store == nil {
		return []remote.Connection{}
	}
	if wsID == "" {
		return store.All()
	}
	return store.List(wsID)
}

// OpenSSH 为指定连接弹出交互式 SSH 终端窗口（ssh -t，BatchMode 恒定，绝不卡密码提示）。
// 窗口标题 "kshell · <连接名>"，同一连接重复调用复用聚焦。
func (a *App) OpenSSH(connID string) error {
	c, ok := a.connByID(connID)
	if !ok {
		return errConnNotFound
	}
	bin, err := remote.FindSSH()
	if err != nil {
		return err
	}
	return a.launchWindow(a.snapshot().Windows,
		providers.Launch{Path: bin, Args: remote.ShellArgs(c, a.sshOptions()), Dir: a.sshDir(c)},
		c.Name,
	)
}

// sshDir 返回 SSH 窗口的起始目录：绑定工作区优先，否则用户主目录。
// home 优先取装配时确定的 a.opts.Home（保持与扫描等路径一致），拿不到再查系统。
func (a *App) sshDir(c remote.Connection) string {
	if c.Workspace != "" {
		return c.Workspace
	}
	if home := a.snapshot().Home; home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// ExecRemote 在指定连接上执行命令并捕获输出（非交互，超时由配置控制）。
// 返回码非 0 不算错误，结果里带 ExitCode 供前端展示。
func (a *App) ExecRemote(connID, cmd string) (remote.Result, error) {
	if a.snapshot().Store == nil {
		return remote.Result{}, errNotReady // 未装配：零值配置会丢掉命令超时保护
	}
	c, ok := a.connByID(connID)
	if !ok {
		return remote.Result{}, errConnNotFound
	}
	return remote.Run(a.runCtx(), c, cmd, a.sshOptions())
}

// runCtx 返回 Wails 生命周期 context；未跑 Startup（测试注入）时兜底 Background。
func (a *App) runCtx() context.Context {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
