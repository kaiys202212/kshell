package workspace

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestGitStatusAtTimeoutIsReported 覆盖 rev-parse 超时误判：git 挂起（网络盘/
// 锁死的仓库）时 rev-parse 超时，gitStatusAt 原先把执行失败一律当「非仓库」
// 静默返回，用户看到的是「没有 git 标记」而不知是仓库卡死。超时必须以 error
// 浮出（与 status 读取路径的超时区分对齐）。
func TestGitStatusAtTimeoutIsReported(t *testing.T) {
	old := gitStatusTimeout
	gitStatusTimeout = 100 * time.Millisecond
	defer func() { gitStatusTimeout = old }()

	// 挂起 runner：rev-parse 阻塞到 ctx 超时
	hang := func(ctx context.Context, root string, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, _, err := gitStatusAtWith(context.Background(), t.TempDir(), hang)
	if err == nil {
		t.Fatal("rev-parse 超时应返回 error，而非静默判为非仓库")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应保留超时语义, got %v", err)
	}
}

// TestGitStatusAtNonRepoIsSilent 保证非仓库路径不回归：rev-parse 普通失败
//（退出码非 0、无 ctx 超时）仍静默判为非仓库。
func TestGitStatusAtNonRepoIsSilent(t *testing.T) {
	notRepo := func(ctx context.Context, root string, args ...string) ([]byte, error) {
		return nil, errors.New("exit status 128: not a git repository")
	}
	status, isRepo, err := gitStatusAtWith(context.Background(), t.TempDir(), notRepo)
	if err != nil || isRepo || status != nil {
		t.Fatalf("非仓库应静默: (%v, %v, %v)", status, isRepo, err)
	}
}
