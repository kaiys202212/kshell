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

var errEmptyPath = errors.New("err.launcher.no_exec")

// Spec 是一次可直接交给 exec 的启动描述（已完成 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string // 额外环境变量（"K=V"）；空表示仅继承
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
	// 上下文取消后最多再等这么久就强制结束，避免子进程不退出时一直挂着。
	cmd.WaitDelay = 2 * time.Second
	return cmd
}
