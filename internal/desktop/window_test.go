package desktop

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubLauncher 记录 Launch/Focus 调用，便于断言
type stubLauncher struct {
	launches  []launchCall
	focuses   []string
	launchErr error
	focusRet  bool
}

type launchCall struct {
	dir   string
	title string
	args  []string
}

func (s *stubLauncher) Launch(dir, title string, args []string) error {
	s.launches = append(s.launches, launchCall{dir: dir, title: title, args: args})
	return s.launchErr
}

func (s *stubLauncher) Focus(title string) bool {
	s.focuses = append(s.focuses, title)
	return s.focusRet
}

func TestTerminalTitle(t *testing.T) {
	m := NewWindowManager(&stubLauncher{}, nil)

	if got := m.TerminalTitle("会话A"); got != "kshell · 会话A" {
		t.Fatalf("TerminalTitle 普通文本 = %q, 期望 %q", got, "kshell · 会话A")
	}

	// 超长按 80 截断并补省略号
	long := strings.Repeat("长", 100)
	got := m.TerminalTitle(long)
	runes := []rune(got)
	if len(runes) != 80 {
		t.Fatalf("TerminalTitle 超长截断后 rune 数 = %d, 期望 80", len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("TerminalTitle 超长截断应以省略号结尾, got %q", got)
	}
	if !strings.HasPrefix(got, "kshell · ") {
		t.Fatalf("TerminalTitle 超长截断应保留前缀, got %q", got)
	}

	// 恰好 80 rune 不截断
	exact := "kshell · " + strings.Repeat("字", 71)
	if got := m.TerminalTitle(strings.Repeat("字", 71)); got != exact {
		t.Fatalf("TerminalTitle 恰好 80 rune 不应截断, got rune 数 %d", len([]rune(got)))
	}
}

func TestManagerLaunchRegistersAlive(t *testing.T) {
	st := &stubLauncher{}
	m := NewWindowManager(st, nil)
	m.newChecker = func(title string) procChecker {
		return func(string) bool { return true }
	}

	if err := m.LaunchSession("d:/work/ws", "标题"); err != nil {
		t.Fatalf("LaunchSession 返回错误: %v", err)
	}

	if !m.Alive("kshell · 标题") {
		t.Fatal("LaunchSession 后 Alive 应为 true")
	}
	if len(st.launches) != 1 {
		t.Fatalf("launcher.Launch 调用次数 = %d, 期望 1", len(st.launches))
	}
	call := st.launches[0]
	if call.dir != "d:/work/ws" {
		t.Fatalf("launcher 收到 dir = %q, 期望 %q", call.dir, "d:/work/ws")
	}
	if call.title != "kshell · 标题" {
		t.Fatalf("launcher 收到 title = %q, 期望 %q", call.title, "kshell · 标题")
	}
}

func TestManagerFocusDelegatesToLauncher(t *testing.T) {
	st := &stubLauncher{focusRet: true}
	m := NewWindowManager(st, nil)
	m.newChecker = func(title string) procChecker {
		return func(string) bool { return true }
	}
	if err := m.LaunchSession("d:/ws", "标题"); err != nil {
		t.Fatalf("LaunchSession 返回错误: %v", err)
	}

	if !m.Focus("kshell · 标题") {
		t.Fatal("Focus 应透传 launcher.Focus 的 true")
	}
	if len(st.focuses) != 1 || st.focuses[0] != "kshell · 标题" {
		t.Fatalf("launcher.Focus 收到 %v, 期望 [kshell · 标题]", st.focuses)
	}
}

func TestManagerFocusUnknownReturnsFalse(t *testing.T) {
	st := &stubLauncher{focusRet: true}
	m := NewWindowManager(st, nil)

	if m.Focus("kshell · 未知") {
		t.Fatal("未知标题 Focus 应返回 false")
	}
	if len(st.focuses) != 0 {
		t.Fatalf("未知标题不应调用 launcher.Focus, 实际调用 %d 次", len(st.focuses))
	}
}

func TestManagerReapRemovesDeadWindows(t *testing.T) {
	st := &stubLauncher{}
	var closed []string
	m := NewWindowManager(st, func(title string) { closed = append(closed, title) })
	states := map[string]bool{"kshell · a": true, "kshell · b": false}
	m.newChecker = func(title string) procChecker {
		return func(string) bool { return states[title] }
	}
	if err := m.LaunchSession("d", "a"); err != nil {
		t.Fatalf("LaunchSession(a) 返回错误: %v", err)
	}
	if err := m.LaunchSession("d", "b"); err != nil {
		t.Fatalf("LaunchSession(b) 返回错误: %v", err)
	}

	got := m.Reap()
	if !reflect.DeepEqual(got, []string{"kshell · b"}) {
		t.Fatalf("Reap 返回 %v, 期望 [kshell · b]", got)
	}
	if !m.Alive("kshell · a") {
		t.Fatal("存活项 a 不应被 Reap 移除")
	}
	if m.Alive("kshell · b") {
		t.Fatal("死亡项 b 应被 Reap 移除")
	}
	if !reflect.DeepEqual(closed, []string{"kshell · b"}) {
		t.Fatalf("onClosed 回调收到 %v, 期望 [kshell · b]", closed)
	}
}

func TestManagerDuplicateLaunchReuses(t *testing.T) {
	st := &stubLauncher{}
	m := NewWindowManager(st, nil)
	m.newChecker = func(title string) procChecker {
		return func(string) bool { return true }
	}
	if err := m.LaunchSession("d", "x"); err != nil {
		t.Fatalf("第一次 LaunchSession 返回错误: %v", err)
	}
	if err := m.LaunchSession("d", "x"); err != nil {
		t.Fatalf("第二次 LaunchSession 返回错误: %v", err)
	}

	if len(st.launches) != 1 {
		t.Fatalf("重复 LaunchSession 后 launcher.Launch 次数 = %d, 期望 1", len(st.launches))
	}
	if len(st.focuses) != 1 || st.focuses[0] != "kshell · x" {
		t.Fatalf("重复 LaunchSession 应转 Focus, launcher.Focus 收到 %v", st.focuses)
	}
}

func TestManagerLaunchErrorNotRegistered(t *testing.T) {
	st := &stubLauncher{launchErr: errors.New("boom")}
	m := NewWindowManager(st, nil)

	if err := m.LaunchSession("d", "x"); err == nil {
		t.Fatal("Launch 失败应向上返回错误")
	}
	if m.Alive("kshell · x") {
		t.Fatal("Launch 失败不应登记存活表")
	}
}
