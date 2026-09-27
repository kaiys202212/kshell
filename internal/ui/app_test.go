package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestAppRendersThreeRegions(t *testing.T) {
	m := NewModel()
	out := m.View()

	for _, want := range []string{"kshell", "Sessions", "Files", "Remote", "PREVIEW"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 rendered lines, got %d:\n%s", len(lines), out)
	}
}

func TestThemeDropsColorWhenNoColorSet(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme := NewTheme()
	for name, style := range map[string]lipgloss.Style{
		"Title":     theme.Title,
		"Tab":       theme.Tab,
		"TabActive": theme.TabActive,
		"Header":    theme.Header,
		"Muted":     theme.Muted,
	} {
		if got := style.Render("kshell"); strings.Contains(got, "\x1b[") {
			t.Fatalf("%s emitted ANSI escape under NO_COLOR: %q", name, got)
		}
	}
}

func TestAppCollapsesToSingleColumnOnSmallTerminal(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("expected Model, got %T", next)
	}

	out := got.View()
	leftWidth := lipgloss.Width(out)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > leftWidth {
			leftWidth = w
		}
	}
	if leftWidth > 40 {
		t.Fatalf("small terminal should render within 40 cols, got %d:\n%s", leftWidth, out)
	}
	if !strings.Contains(out, "PREVIEW") {
		t.Fatalf("collapsed view should keep the left list, got:\n%s", out)
	}
}
