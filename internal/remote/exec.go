package remote

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

var errSSHNotFound = errors.New("err.ssh.no_ssh_binary")

// SSHOptions 影响参数拼装；BatchMode 不开放配置——kshell 永远不允许卡在密码提示上。
type SSHOptions struct {
	ConnectTimeout        int
	ExtraArgs             []string
	CommandTimeoutSeconds int
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

func FindSSH() (string, error) {
	bin, err := exec.LookPath("ssh")
	if err != nil || bin == "" {
		return "", errSSHNotFound
	}
	return bin, nil
}

// BuildArgs 拼装 ssh 参数：BatchMode + 连接超时保证不会挂起等待输入。
func BuildArgs(c Connection, cmd string, opts SSHOptions) []string {
	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = 5
	}

	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=" + strconv.Itoa(timeout)}
	args = append(args, opts.ExtraArgs...)

	if c.Port > 0 && c.Port != 22 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	if strings.TrimSpace(c.IdentityFile) != "" {
		args = append(args, "-i", c.IdentityFile)
	}

	target := c.Host
	if c.User != "" {
		target = c.User + "@" + c.Host
	}
	args = append(args, target)

	if strings.TrimSpace(cmd) != "" {
		args = append(args, "--", cmd)
	}
	return args
}

// ShellArgs 供交互式登录使用（-t 强制分配伪终端）。
// 契约：BuildArgs 的前 4 个参数恒为 -o BatchMode=yes -o ConnectTimeout=N 两对
// （不受 opts/连接影响），此处按下标在其后插入 -t；改动 BuildArgs 前段时必须同步这里。
func ShellArgs(c Connection, opts SSHOptions) []string {
	base := BuildArgs(c, "", opts)
	args := []string{base[0], base[1], base[2], base[3], "-t"}
	args = append(args, base[4:]...)
	return args
}

func Run(ctx context.Context, c Connection, cmd string, opts SSHOptions) (Result, error) {
	bin, err := FindSSH()
	if err != nil {
		return Result{}, err
	}

	if opts.CommandTimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.CommandTimeoutSeconds)*time.Second)
		defer cancel()
	}

	var stdout, stderr strings.Builder
	cmdExec := exec.CommandContext(ctx, bin, BuildArgs(c, cmd, opts)...)
	executil.HideWindow(cmdExec) // GUI 壳下 ssh 调用不闪黑窗
	cmdExec.Stdout = &stdout
	cmdExec.Stderr = &stderr
	cmdExec.WaitDelay = 2 * time.Second

	start := time.Now()
	runErr := cmdExec.Run()
	res := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}

	if ctx.Err() != nil {
		// 被取消/超时：进程是被我们杀掉的，不是命令自己失败，必须让调用方知道。
		return res, fmt.Errorf("err.ssh.exec_timeout|%w", ctx.Err())
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("err.ssh.exec_failed|%w", runErr)
	}
	return res, nil
}

// Target 展示用的 user@host:port。
func (c Connection) Display() string {
	target := c.Target()
	if c.Port > 0 && c.Port != 22 {
		target += ":" + strconv.Itoa(c.Port)
	}
	return target
}
