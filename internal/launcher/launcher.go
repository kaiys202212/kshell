package launcher

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
	"github.com/yangk/kshell/internal/providers"
)

var errEmptyPath = errors.New("launcher: 未指定可执行文件（工具只检测到配置目录，没有可执行程序）")

// Spec 是一次可直接交给 exec 的启动描述（已完成 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string // 额外环境变量（"K=V"）；空表示仅继承
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Build 把 provider 产出的 Launch 变成可执行 Spec：校验路径并做平台相关的 shim 解析。
func Build(l providers.Launch) (Spec, error) {
	if strings.TrimSpace(l.Path) == "" {
		return Spec{}, errEmptyPath
	}
	spec := Resolve(l.Path, l.Args)
	spec.Dir = l.Dir
	spec.Env = providers.EnvList(l.Env)
	return spec, nil
}

func (s Spec) Cmd(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.Path, s.Args...)
	executil.HideWindow(cmd) // GUI 壳下调用不闪黑窗；TUI 下子进程继承父控制台，无副作用
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}
	if len(s.Env) > 0 {
		cmd.Env = append(os.Environ(), s.Env...)
	}
	// 上下文取消后最多再等这么久就强制结束，避免子进程不退出时 Run 一直挂着。
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// Run 非交互式执行并捕获输出；进程返回非 0 不算错误，返回码放在 Result 里给 UI 展示。
func Run(ctx context.Context, s Spec) (Result, error) {
	var stdout, stderr strings.Builder

	cmd := s.Cmd(ctx)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	err := cmd.Run()
	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Tail 取输出最后 n 行，供 UI 在会话启动失败时展示原因。
func (r Result) Tail(n int) string {
	lines := strings.Split(strings.TrimRight(r.Stdout+"\n"+r.Stderr, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
