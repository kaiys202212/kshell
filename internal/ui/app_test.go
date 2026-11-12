package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/workbuddy"
)

func TestAppRendersThreeRegions(t *testing.T) {
	m := NewModel()
	out := m.View()

	for _, want := range []string{"Capybara", "Sessions", "Files", "Remote", "PREVIEW"} {
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

// TestModelOptionsPlatformInjection 覆盖 TUI 侧模型注入的平台语义：
// 端点与密钥取激活平台（ActivePlatformID 命中，否则第一个），
// 不注入模型名（默认模型由 agent 自行决定）；无平台时不命中，未启用时不注入。
func TestModelOptionsPlatformInjection(t *testing.T) {
	newWithModel := func(mc config.ModelConfig) Model {
		return NewModelWith(Options{Config: config.Config{MaxDepth: 10, Model: mc}})
	}

	// ActivePlatformID 命中：注入该平台端点与密钥，ActiveModel 不注入。
	m := newWithModel(config.ModelConfig{
		Enabled:         true,
		ActivePlatformID: "p1",
		ActiveModel:     "glm-5",
		Platforms: []config.ModelPlatform{
			{ID: "p1", OpenAIBaseURL: "https://oai/", AnthropicBaseURL: "https://ant/", APIKey: "k"},
			{ID: "p2", OpenAIBaseURL: "https://oai2/", APIKey: "k2"},
		},
	})
	mc, ok := m.modelOptions("claude").Resolver("claude")
	if !ok || mc.Model != "" || mc.OpenAIBaseURL != "https://oai/" || mc.AnthropicBaseURL != "https://ant/" || mc.APIKey != "k" {
		t.Fatalf("resolver(claude) = %+v ok=%v", mc, ok)
	}

	// ActivePlatformID 为空：回落第一个平台。
	m = newWithModel(config.ModelConfig{
		Enabled:   true,
		Platforms: []config.ModelPlatform{{ID: "p1", OpenAIBaseURL: "https://oai1/", APIKey: "k1"}},
	})
	mc, ok = m.modelOptions("claude").Resolver("claude")
	if !ok || mc.OpenAIBaseURL != "https://oai1/" || mc.APIKey != "k1" {
		t.Fatalf("应回落第一个平台: %+v ok=%v", mc, ok)
	}

	// 启用但无平台：解析委托 modelresolve，Resolver 存在但调用时不命中。
	mo := newWithModel(config.ModelConfig{Enabled: true}).modelOptions("claude")
	if mo.Resolver == nil {
		t.Fatal("启用时应有 resolver（平台解析延迟到调用时）")
	}
	if _, ok := mo.Resolver("claude"); ok {
		t.Fatal("无平台时不应命中")
	}

	// 未启用：直接不注入。
	if got := newWithModel(config.ModelConfig{Enabled: false, Platforms: []config.ModelPlatform{{ID: "p1"}}}).modelOptions("claude").Resolver; got != nil {
		t.Fatal("未启用不应注入")
	}
}

// tuiPerToolConfig 构造 TUI 每工具平台测试配置：全局激活 p2，codex 映射 p1。
// 端点形状与 modelresolve/desktop 测试保持一致，便于对照口径。
func tuiPerToolConfig() config.ModelConfig {
	return config.ModelConfig{
		Enabled:          true,
		ActivePlatformID: "p2",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P1", OpenAIBaseURL: "https://o1/v1", AnthropicBaseURL: "https://a1/v1", APIKey: "k1"},
			{ID: "p2", Name: "P2", OpenAIBaseURL: "https://o2/v1", AnthropicBaseURL: "https://a2/v1", APIKey: "k2"},
		},
		ToolSync: config.ToolSyncConfig{ToolPlatforms: map[string]string{"codex": "p1"}},
	}
}

// tuiModelWithCodex 准备一个只有 codex 可用的 TUI 模型（含一个 codex 会话）。
func tuiModelWithCodex(cfg config.ModelConfig) Model {
	sessions := []providers.Session{{
		ID: "s-codex", ToolID: "codex", Workspace: `D:\ws-codex`,
		Title: "codex 会话", UpdatedAt: time.Now(),
	}}
	m := NewModelWith(Options{Config: config.Config{MaxDepth: 10, Model: cfg}})
	m.tools = []discovery.Tool{{ID: "codex", Installed: true, BinPath: "codex"}}
	m.workspaces = discovery.GroupSessions(sessions)
	m.sessions = sessions
	m.focus = focusWorkspaces
	return m
}

// fakeTUIProxier 是 modelresolve.Proxier 的测试替身，记录 Ensure 调用次数。
type fakeTUIProxier struct {
	base, token string
	calls       int
}

func (f *fakeTUIProxier) Ensure() (string, string, error) {
	f.calls++
	return f.base, f.token, nil
}

// TUI 模型注入必须按工具解析平台：codex 映射 p1 时，即便全局激活 p2，
// 新建会话也吃 p1 端点；恢复路径的来源同样以会话 ToolID 为准。
// 注意恢复路径本身维持 launch.ForSession「不注入模型」现状（见下方断言）。
func TestModelOptionsPerToolTUI(t *testing.T) {
	m := tuiModelWithCodex(tuiPerToolConfig())

	// 恢复路径的 modelOptions 来源必须是会话所属工具，而不是全局激活平台。
	s, ok := m.selectedSession()
	if !ok {
		t.Fatal("应选中 codex 会话")
	}
	mc, ok := m.modelOptions(s.ToolID).Resolver(s.ToolID)
	if !ok || mc.OpenAIBaseURL != "https://o1/v1" || mc.APIKey != "k1" {
		t.Fatalf("modelOptions(codex) = %+v ok=%v, want p1 端点", mc, ok)
	}

	// 新建会话经 PreferredTool 解析首选工具（codex），同样按 p1 注入环境变量。
	l, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if got := l.Env["OPENAI_BASE_URL"]; got != "https://o1/v1" {
		t.Fatalf("新建会话 OPENAI_BASE_URL = %q, want https://o1/v1（env=%v）", got, l.Env)
	}
	if l.Env["OPENAI_API_KEY"] != "k1" {
		t.Fatalf("新建会话 OPENAI_API_KEY = %q, want k1（env=%v）", l.Env["OPENAI_API_KEY"], l.Env)
	}

	// 边界：恢复路径沿用会话原模型，launch.ForSession 故意不注入模型端点。
	rl, err := m.resumeLaunch()
	if err != nil {
		t.Fatalf("resumeLaunch error: %v", err)
	}
	if v, ok := rl.Env["OPENAI_BASE_URL"]; ok {
		t.Fatalf("恢复会话不应注入模型端点，got %q（env=%v）", v, rl.Env)
	}
}

// 激活 workbuddy 平台时，TUI 的 Resolver 必须经注入的 Proxier 取端点与令牌。
func TestModelOptionsTUIUsesProxierForWorkbuddy(t *testing.T) {
	cfg := tuiPerToolConfig()
	cfg.ActivePlatformID = "wb"
	cfg.Platforms = append(cfg.Platforms, config.ModelPlatform{ID: "wb", Name: "WorkBuddy", Preset: workbuddy.PresetID})
	// 去掉 codex 映射，让解析回落到全局激活的 workbuddy 平台。
	delete(cfg.ToolSync.ToolPlatforms, "codex")
	m := tuiModelWithCodex(cfg)
	px := &fakeTUIProxier{base: "http://127.0.0.1:9999", token: "tk"}
	m.modelProxy = px

	l, err := m.newSessionLaunch()
	if err != nil {
		t.Fatalf("newSessionLaunch error: %v", err)
	}
	if px.calls == 0 {
		t.Fatal("workbuddy 平台应经 Proxier.Ensure 取端点")
	}
	if l.Env["OPENAI_BASE_URL"] != px.base || l.Env["OPENAI_API_KEY"] != px.token {
		t.Fatalf("env = %v, want 代理端点 %q / 令牌 %q", l.Env, px.base, px.token)
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
