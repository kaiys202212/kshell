package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yangk/kshell/internal/appearance"
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

func TestViewNamesMatchViewCount(t *testing.T) {
	if len(viewNames) != int(viewCount) {
		t.Fatalf("viewNames(%d) 与 viewCount(%d) 不一致，加视图时两处都要改", len(viewNames), int(viewCount))
	}
}

func TestThemeDropsColorWhenNoColorSet(t *testing.T) {
	// 钉死彩色 profile，否则非 TTY 环境下本测试永远为真（实现不处理 NO_COLOR 也会通过）。
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	t.Setenv("NO_COLOR", "1")
	theme := NewTheme(appearance.Dark)

	for name, style := range map[string]lipgloss.Style{
		"Title":     theme.Title,
		"Tab":       theme.Tab,
		"TabActive": theme.TabActive,
		"Header":    theme.Header,
		"Muted":     theme.Muted,
		"Body":      theme.Body,
		"Preview":   theme.Preview,
		"StatusBar": theme.StatusBar,
	} {
		if got := style.Render("kshell"); strings.Contains(got, "\x1b[") {
			t.Fatalf("%s 在 NO_COLOR 下仍输出 SGR: %q", name, got)
		}
	}
}

func TestNewThemeLightAndDarkDiffer(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	t.Setenv("NO_COLOR", "")

	dark := NewTheme(appearance.Dark).Body.Render("x")
	light := NewTheme(appearance.Light).Body.Render("x")
	if dark == light {
		t.Fatalf("light 与 dark 正文渲染不应相同: %q", dark)
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
	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("want 12 lines (高度守恒), got %d:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 40 {
			t.Fatalf("line %d width = %d, want 40: %q", i, w, line)
		}
	}

	wsRow, wsOK := rowOf(lines, "WORKSPACES")
	pvRow, pvOK := rowOf(lines, "PREVIEW")
	if !wsOK || !pvOK {
		t.Fatalf("小屏视图缺少列表或预览区块:\n%s", out)
	}
	if wsRow == pvRow {
		t.Fatalf("小屏应纵向堆叠，WORKSPACES 与 PREVIEW 不应在同一行:\n%s", out)
	}
}

func TestFitBlockTruncatesOverlongLine(t *testing.T) {
	got := fitBlock([]string{strings.Repeat("x", 30)}, 2, 10)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 10 {
			t.Fatalf("line %d width = %d, want 10: %q", i, w, line)
		}
	}
}

func TestJoinHorizontalFitKeepsWidth(t *testing.T) {
	got := joinHorizontalFit(20, strings.Repeat("a", 20), strings.Repeat("b", 21))
	if w := lipgloss.Width(got); w > 20 {
		t.Fatalf("width = %d, want <= 20: %q", w, got)
	}
}

func rowOf(lines []string, substr string) (int, bool) {
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i, true
		}
	}
	return -1, false
}
