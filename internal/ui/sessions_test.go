package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

func modelWithSessions() Model {
	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: "D:\\ws-a", Title: "修复上传白名单", UpdatedAt: time.Now().Add(-time.Hour), Messages: 12},
		{ID: "s2", ToolID: "codex", Workspace: "D:\\ws-a", Title: "沙箱 execute 约束", UpdatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "s3", ToolID: "claude", Workspace: "D:\\ws-b", Title: "别的工程的会话", UpdatedAt: time.Now()},
	}

	m := NewModel()
	m.workspaces = discovery.GroupSessions(sessions)
	m.sessions = sessions
	m.tools = []discovery.Tool{{ID: "claude", Installed: true, BinPath: "claude", Version: "2.1.81"}}
	return m
}

func TestSessionsViewListsOnlySelectedWorkspaceSessions(t *testing.T) {
	m := modelWithSessions()
	out := m.View()

	// 默认选中最近使用的工作区（ws-b，因为 s3 最新）
	if !strings.Contains(out, "别的工程的会话") {
		t.Fatalf("view should list sessions of the selected workspace:\n%s", out)
	}
	if strings.Contains(out, "修复上传白名单") {
		t.Fatalf("sessions of other workspaces must not leak in:\n%s", out)
	}
}

func TestSelectingSecondWorkspaceSwitchesSessions(t *testing.T) {
	m := modelWithSessions()
	// 工作区按最近使用排序：ws-b(0) -> ws-a(1)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := next.(Model)

	sessions := got.sessionsOfSelected()
	if len(sessions) != 2 {
		t.Fatalf("ws-a should have 2 sessions, got %d", len(sessions))
	}
	for _, s := range sessions {
		if s.Workspace != "D:\\ws-a" {
			t.Fatalf("unexpected session from %q", s.Workspace)
		}
	}
}

func TestFilterNarrowsSessionList(t *testing.T) {
	m := modelWithSessions()
	m.focus = focusSessions
	m.wsCursor = 1 // ws-a
	m.filter = "沙箱"

	got := m.sessionsOfSelected()
	if len(got) != 1 || got[0].ID != "s2" {
		t.Fatalf("filtered sessions = %+v, want only s2", got)
	}

	// 过滤会话时工作区列表必须保持完整，否则没法切换到别的工作区
	if len(m.visibleWorkspaces()) != 2 {
		t.Fatalf("workspaces = %+v, want all 2 while filtering sessions", m.visibleWorkspaces())
	}
}

func TestFilterNarrowsWorkspaceListWhenFocused(t *testing.T) {
	m := modelWithSessions()
	m.focus = focusWorkspaces
	m.filter = "ws-b"

	got := m.visibleWorkspaces()
	if len(got) != 1 || got[0].Path != "D:\\ws-b" {
		t.Fatalf("filtered workspaces = %+v, want only ws-b", got)
	}
}

func TestFilterTypingAndEscape(t *testing.T) {
	m := modelWithSessions()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := next.(Model)
	if !got.filtering {
		t.Fatal("pressing / should enter filter mode")
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	got = next.(Model)
	if got.filter != "ab" {
		t.Fatalf("filter = %q, want ab", got.filter)
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(Model)
	if got.filtering || got.filter != "" {
		t.Fatalf("esc should clear the filter, got filtering=%v filter=%q", got.filtering, got.filter)
	}
}

func TestPreviewShowsSelectedSessionSummary(t *testing.T) {
	m := modelWithSessions()
	m.focus = focusSessions

	out := m.View()
	for _, want := range []string{"PREVIEW", "别的工程的会话", "claude", "s3", "D:\\ws-b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview missing %q:\n%s", want, out)
		}
	}
}

func TestResumeLaunchUsesRealBinary(t *testing.T) {
	m := modelWithSessions()
	m.focus = focusSessions

	launch, err := m.resumeLaunch()
	if err != nil {
		t.Fatalf("resumeLaunch error: %v", err)
	}
	if launch.Path != "claude" {
		t.Fatalf("path = %q", launch.Path)
	}
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "s3" {
		t.Fatalf("args = %v, want [--resume s3]", launch.Args)
	}
	if launch.Dir != "D:\\ws-b" {
		t.Fatalf("dir = %q, want the session workspace", launch.Dir)
	}
}

func TestResumeLaunchFailsWithoutRunnableTool(t *testing.T) {
	m := modelWithSessions()
	m.focus = focusSessions
	m.tools = []discovery.Tool{{ID: "claude", Installed: true, BinPath: ""}} // 只有配置目录，没有可执行文件

	if _, err := m.resumeLaunch(); err == nil {
		t.Fatal("resume must fail when the tool has no executable")
	}
}

func TestNewSessionLaunchHasNoExtraArgs(t *testing.T) {
	m := modelWithSessions()

	launch, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if len(launch.Args) != 0 {
		t.Fatalf("args = %v, want no extra args", launch.Args)
	}
}

func TestScanResultFillsModelAndStatus(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(scanDoneMsg{
		tools:      []discovery.Tool{{ID: "claude", Installed: true}},
		sessions:   []providers.Session{{ID: "x", Workspace: "D:\\ws"}},
		workspaces: []discovery.Workspace{{Path: "D:\\ws", Name: "ws"}},
		failed:     2,
	})
	got := next.(Model)

	if len(got.sessions) != 1 || len(got.workspaces) != 1 {
		t.Fatalf("model not filled: %+v", got)
	}
	if !strings.Contains(got.status, "2") {
		t.Fatalf("status should mention failed parses, got %q", got.status)
	}
}
