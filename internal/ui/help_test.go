package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpPanelToggles(t *testing.T) {
	m := NewModel()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := next.(Model)
	if !got.showHelp {
		t.Fatal("? should open help")
	}

	out := got.View()
	for _, want := range []string{"键位", "Remote 视图", "退出 kshell"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}

	// 再按一次关闭
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got = next.(Model)
	if got.showHelp {
		t.Fatal("? again should close help")
	}

	// esc 也能关闭
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got = next.(Model)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(Model)
	if got.showHelp {
		t.Fatal("esc should close help")
	}
}

func TestHelpMentionsEveryRemoteKey(t *testing.T) {
	out := renderHelp(40, 100, NewTheme())
	for _, key := range []string{"i ", "x ", "s ", "t ", "b ", "d ", "space", "a "} {
		if !strings.Contains(out, key) {
			t.Fatalf("help should document %q", key)
		}
	}
}
