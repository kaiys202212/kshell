package desktop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
)

// autoResumeConn：首轮返回 429，后续成功；用于验证生产装配确实开启了自动续跑。
type autoResumeConn struct {
	fakeChatConn
	mu      sync.Mutex
	prompts int
}

func (c *autoResumeConn) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	c.mu.Lock()
	c.prompts++
	n := c.prompts
	c.mu.Unlock()
	if n == 1 {
		return "", errors.New("429 too many requests")
	}
	return "end_turn", nil
}

func (c *autoResumeConn) promptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompts
}

type autoResumeBackend struct{ conn *autoResumeConn }

func (b autoResumeBackend) Start(spec chat.Spec, h acp.Handler) (chat.Conn, error) {
	return b.conn, nil
}

func TestNewChatManagerEnablesAutoResume(t *testing.T) {
	old := desktopAutoResumeDelay
	desktopAutoResumeDelay = 0
	t.Cleanup(func() { desktopAutoResumeDelay = old })

	app, _, _ := newTestApp(t)
	conn := &autoResumeConn{fakeChatConn: fakeChatConn{waitCh: make(chan struct{})}}
	m := newChatManager(app, autoResumeBackend{conn})
	t.Cleanup(m.CloseAll)
	info, err := m.Open("new:1", chat.Info{Kind: chat.KindNew, Workspace: "/w"}, chat.Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if conn.promptCount() >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("prompt count = %d, want auto resume to fire", conn.promptCount())
}
