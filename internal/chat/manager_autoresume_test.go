package chat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const autoResumeTextForTest = "继续未完成的任务"

func newAutoResumeTestManager(t *testing.T, c *fakeConn) *Manager {
	t.Helper()
	b := &fakeBackend{conn: c}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	m.SetAutoResume(true, 0, func() string { return autoResumeTextForTest })
	return m
}

func promptAt(c *fakeConn, i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.prompts) {
		return ""
	}
	return c.prompts[i]
}

func TestAutoResumeOnTransientError(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests"), nil}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.Prompt(info.ID, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitFor(t, func() bool { return c.promptCount() == 2 })
	if got := promptAt(c, 1); got != autoResumeTextForTest {
		t.Fatalf("second prompt = %q, want %q", got, autoResumeTextForTest)
	}
}

func TestAutoResumeStopsAfterOneAttempt(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests"), errors.New("503 unavailable")}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool { return c.promptCount() == 2 })
	time.Sleep(50 * time.Millisecond)
	if n := c.promptCount(); n != 2 {
		t.Fatalf("prompt count = %d, want 2", n)
	}
}

func TestAutoResumeSkipsDeterministicError(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("invalid params: model not found")}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool { return c.promptCount() == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := c.promptCount(); n != 1 {
		t.Fatalf("prompt count = %d, want 1", n)
	}
}

func TestAutoResumeCanceledByNewUserPrompt(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests"), nil}}
	b := &fakeBackend{conn: c}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	m.SetAutoResume(true, 100*time.Millisecond, func() string { return autoResumeTextForTest })
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool {
		cur, ok := m.Get(info.ID)
		return ok && cur.Status == StatusReady
	})
	_ = m.Prompt(info.ID, "用户新输入")
	waitFor(t, func() bool { return c.promptCount() == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := c.promptCount(); n != 2 {
		t.Fatalf("prompt count = %d, want 2", n)
	}
	if got := promptAt(c, 1); !strings.Contains(got, "用户新输入") {
		t.Fatalf("second prompt = %q, want user input", got)
	}
}

func TestAutoResumeSkipsWhenUserQueued(t *testing.T) {
	gate := make(chan struct{})
	c := &fakeConn{waitCh: make(chan struct{}), promptGate: gate, promptErrs: []error{errors.New("429 too many requests"), nil}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "first")
	waitFor(t, func() bool { return c.promptCount() == 1 })
	_ = m.Prompt(info.ID, "用户排队消息")
	close(gate)
	waitFor(t, func() bool { return c.promptCount() == 2 })
	if got := promptAt(c, 1); !strings.Contains(got, "用户排队消息") {
		t.Fatalf("second prompt = %q, want queued user message", got)
	}
	time.Sleep(50 * time.Millisecond)
	if n := c.promptCount(); n != 2 {
		t.Fatalf("prompt count = %d, want 2", n)
	}
}

func TestAutoResumeOnRefusalWithTransientStderr(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), stop: "refusal", stderr: "error: 429 too many requests"}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool { return c.promptCount() == 2 })
}

func TestAutoResumeDisabledByDefault(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests")}}
	b := &fakeBackend{conn: c}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	time.Sleep(100 * time.Millisecond)
	if n := c.promptCount(); n != 1 {
		t.Fatalf("prompt count = %d, want 1", n)
	}
}
