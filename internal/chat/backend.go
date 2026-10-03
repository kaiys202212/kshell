package chat

import (
	"context"

	"github.com/yangk/kshell/internal/acp"
)

// RealBackend 用 internal/acp 启动真实 agent 进程。
type RealBackend struct{}

func (RealBackend) Start(spec Spec, h acp.Handler) (Conn, error) {
	return acp.NewAgent(context.Background(), acp.Spec{Path: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env}, h)
}
