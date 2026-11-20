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

func TestAutoResumeIgnoresPreviousTurnStderr(t *testing.T) {
	c := &fakeConn{
		waitCh:         make(chan struct{}),
		stderrOnPrompt: []string{"old error: 429 too many requests", ""},
		promptErrs:     []error{errors.New("invalid params"), errors.New("Internal error")},
	}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool {
		cur, ok := m.Get(info.ID)
		return ok && cur.Status == StatusReady
	})
	_ = m.Prompt(info.ID, "用户再次输入")
	waitFor(t, func() bool { return c.promptCount() >= 2 })
	time.Sleep(50 * time.Millisecond)
	if n := c.promptCount(); n != 2 {
		t.Fatalf("prompt count = %d, want 2（旧轮 stderr 不得触发续跑）", n)
	}
}

func TestAutoResumeCanceledDuringTurn(t *testing.T) {
	gate := make(chan struct{})
	c := &fakeConn{waitCh: make(chan struct{}), promptGate: gate, promptErrs: []error{errors.New("stream error")}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool { return c.promptCount() == 1 })
	if err := m.Cancel(info.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(gate)
	time.Sleep(100 * time.Millisecond)
	if n := c.promptCount(); n != 1 {
		t.Fatalf("prompt count = %d, want 1（用户取消后不得自动续跑）", n)
	}
}

func TestAutoResumeSkipsDeadConn(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), deadOnPromptErr: true, promptErrs: []error{errors.New("429 too many requests")}}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	time.Sleep(100 * time.Millisecond)
	if n := c.promptCount(); n != 1 {
		t.Fatalf("prompt count = %d, want 1（连接已死不得续跑）", n)
	}
}

func TestAutoResumeTimelineOrder(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests"), nil}}
	b := &fakeBackend{conn: c}
	col := &collector{}
	m := NewManager(b, col.onUpdate, col.onPermission, col.onExit)
	m.SetAutoResume(true, 100*time.Millisecond, func() string { return autoResumeTextForTest })
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool {
		col.mu.Lock()
		defer col.mu.Unlock()
		var seenError, seenAuto bool
		for _, u := range col.updates {
			if u.Type == "error" {
				seenError = true
			}
			if seenError && u.Type == "user" && u.Text == autoResumeTextForTest {
				seenAuto = true
			}
		}
		return seenAuto
	})
	col.mu.Lock()
	defer col.mu.Unlock()
	errIdx, autoIdx := -1, -1
	for i, u := range col.updates {
		if u.Type == "error" && errIdx < 0 {
			errIdx = i
		}
		if u.Type == "user" && u.Text == autoResumeTextForTest && autoIdx < 0 {
			autoIdx = i
		}
	}
	if errIdx < 0 || autoIdx < 0 || errIdx > autoIdx {
		t.Fatalf("时间线顺序错误: error=%d auto=%d updates=%+v", errIdx, autoIdx, col.updates)
	}
}

func TestAutoResumeResetsOnNewUserTurn(t *testing.T) {
	c := &fakeConn{
		waitCh: make(chan struct{}),
		promptErrs: []error{
			errors.New("429 too many requests"),
			errors.New("429 too many requests"),
			errors.New("429 too many requests"),
			errors.New("429 too many requests"),
		},
	}
	m := newAutoResumeTestManager(t, c)
	defer m.CloseAll()
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "first")
	waitFor(t, func() bool { return c.promptCount() == 2 })
	waitFor(t, func() bool {
		cur, ok := m.Get(info.ID)
		return ok && cur.Status == StatusReady
	})
	_ = m.Prompt(info.ID, "second")
	waitFor(t, func() bool { return c.promptCount() == 4 })
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

func TestAutoResumeSkipsPendingPermission(t *testing.T) {
	m := NewManager(&fakeBackend{}, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	m.SetAutoResume(true, 0, func() string { return autoResumeTextForTest })
	s := &session{
		id:            "c1",
		info:          Info{Status: StatusReady},
		conn:          &fakeConn{},
		pending:       map[string]chan permissionResult{"r1": make(chan permissionResult, 1)},
		pendingElicit: map[string]chan elicitationResult{},
	}
	m.byID[s.id] = s
	m.mu.Lock()
	m.scheduleAutoResumeLocked(s, s.autoResumeToken, errors.New("429 too many requests"), "", "")
	m.mu.Unlock()
	if s.autoResumeTimer != nil {
		t.Fatal("有等待中的权限请求时不得安排自动续跑")
	}
}

func TestAutoResumeCanceledByClose(t *testing.T) {
	c := &fakeConn{waitCh: make(chan struct{}), promptErrs: []error{errors.New("429 too many requests")}}
	b := &fakeBackend{conn: c}
	m := NewManager(b, func(string, Update) {}, func(string, PermissionRequest) {}, func(string, int, string) {})
	m.SetAutoResume(true, 100*time.Millisecond, func() string { return autoResumeTextForTest })
	info, _ := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	_ = m.Prompt(info.ID, "hi")
	waitFor(t, func() bool { return c.promptCount() == 1 })
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if n := c.promptCount(); n != 1 {
		t.Fatalf("prompt count = %d, want 1（关闭后不得续跑）", n)
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
	c := &fakeConn{waitCh: make(chan struct{}), stop: "refusal", stderrOnPrompt: []string{"error: 429 too many requests"}}
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
