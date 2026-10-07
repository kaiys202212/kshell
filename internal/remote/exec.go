package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

var errSSHNotFound = errors.New("err.ssh.no_ssh_binary")

// SSHOptions 影响参数拼装。无密码时固定 BatchMode，避免卡在交互提示；
// 有密码时省略 BatchMode，改走 SSH_ASKPASS 非交互回填。
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

// BuildArgs 拼装 ssh 参数。无密码时含 BatchMode=yes；有密码时省略，密码不进 argv。
func BuildArgs(c Connection, cmd string, opts SSHOptions) []string {
	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = 5
	}

	var args []string
	if strings.TrimSpace(c.Password) == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(timeout))
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
// 在全部选项（含 -o/-p/-i 及 ExtraArgs）之后、目标主机之前插入 -t。
func ShellArgs(c Connection, opts SSHOptions) []string {
	base := BuildArgs(c, "", opts)
	optEnd := sshOptionsEnd(base)
	out := make([]string, 0, len(base)+1)
	out = append(out, base[:optEnd]...)
	out = append(out, "-t")
	out = append(out, base[optEnd:]...)
	return out
}

// sshOptionsEnd 返回第一个非选项参数（主机或 --）的下标。
func sshOptionsEnd(args []string) int {
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") {
			return i
		}
		if sshOptTakesValue(a) {
			if i+1 >= len(args) {
				return len(args)
			}
			i += 2
			continue
		}
		i++
	}
	return i
}

func sshOptTakesValue(flag string) bool {
	switch flag {
	case "-o", "-p", "-i", "-l", "-F", "-c", "-D", "-L", "-R", "-W", "-w", "-b", "-e", "-m", "-S", "-E", "-J":
		return true
	default:
		// -oBatchMode=yes 这类连写不占下一参数
		if strings.HasPrefix(flag, "-o") && len(flag) > 2 {
			return false
		}
		return false
	}
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
	if env, err := AskPassEnvSlice(c.Password); err != nil {
		return Result{}, err
	} else if len(env) > 0 {
		cmdExec.Env = append(os.Environ(), env...)
	}
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
